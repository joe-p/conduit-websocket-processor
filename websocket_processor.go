package websocket_processor

import (
	"context"
	_ "embed" // used to embed config
	"fmt"
	"net"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/algorand/conduit/conduit/data"
	"github.com/algorand/conduit/conduit/plugins"
	"github.com/algorand/conduit/conduit/plugins/processors"

	"github.com/algorand/go-algorand-sdk/v2/crypto"
	"github.com/algorand/go-algorand-sdk/v2/encoding/msgpack"
	"github.com/algorand/go-algorand-sdk/v2/types"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

type SavedMaps struct {
	LocalDeltas map[string]map[uint64]types.StateDelta
	Leases      map[types.Txlease]types.Round
	Createables map[types.CreatableIndex]types.ModifiedCreatable
	Spt         map[types.StateProofType]types.StateProofTrackingData
	DeltaSpt    map[types.StateProofType]types.StateProofTrackingData
}

// PluginName to use when configuring.
const PluginName = "websocket_processor"

// package-wide init function
func init() {
	processors.Register(PluginName, processors.ProcessorConstructorFunc(func() processors.Processor {
		return &WebsocketProcessor{}
	}))
}

type WebsocketProcessor struct {
	logger *log.Logger
	ctx    context.Context
	conn   net.Conn
}

// Metadata returns metadata
func (a *WebsocketProcessor) Metadata() plugins.Metadata {
	return plugins.Metadata{
		Name:         PluginName,
		Description:  "Filter transactions out via a lua script.",
		Deprecated:   false,
		SampleConfig: "",
	}
}

// Config returns the config
func (a *WebsocketProcessor) Config() string {
	return ""
}

// Init initializes the filter processor
func (a *WebsocketProcessor) Init(ctx context.Context, _ data.InitProvider, _ plugins.PluginConfig, logger *log.Logger) error {
	a.logger = logger
	a.ctx = ctx
	a.logger.Debug("Initializing websocket processor")

	a.logger.Debug("Listening on localhost:8888")
	listener, err := net.Listen("tcp", "localhost:8888")

	if err != nil {
		return err
	}

	a.logger.Debug("Accepting connections on localhost:8888")
	conn, err := listener.Accept()

	if err != nil {
		return err
	}

	_, err = ws.Upgrade(conn)
	if err != nil {
		return err
	}

	a.conn = conn

	a.logger.Debug("Websocket processor initialized")
	return nil

}

func (a *WebsocketProcessor) Close() error {
	a.conn.Close()
	return nil
}

func removeInnerLocalDeltas(txns *[]types.SignedTxnWithAD, savedLocalDeltas map[string]map[uint64]types.StateDelta) {
	for i := 0; i < len(*txns); i++ {
		txID := crypto.GetTxID((*txns)[i].Txn)

		savedLocalDeltas[txID] = (*txns)[i].EvalDelta.LocalDeltas
		(*txns)[i].EvalDelta.LocalDeltas = nil

		removeInnerLocalDeltas(&(*txns)[i].EvalDelta.InnerTxns, savedLocalDeltas)
	}
}

func restoreInnerLocalDeltas(txns *[]types.SignedTxnWithAD, savedLocalDeltas map[string]map[uint64]types.StateDelta) {
	for i := 0; i < len(*txns); i++ {
		txID := crypto.GetTxID((*txns)[i].Txn)

		(*txns)[i].EvalDelta.LocalDeltas = savedLocalDeltas[txID]

		restoreInnerLocalDeltas(&(*txns)[i].EvalDelta.InnerTxns, savedLocalDeltas)
	}
}

// Process processes the input data
func (a *WebsocketProcessor) Process(input data.BlockData) (data.BlockData, error) {
	// Don't encode the following maps because their encoding is currently broken
	// Should be fixed when the following PR is merged and availible in sdk and conduit
	// https://github.com/algorand/go-codec/pull/4
	savedMaps := SavedMaps{
		Spt:         input.BlockHeader.StateProofTracking,
		DeltaSpt:    input.Delta.Hdr.StateProofTracking,
		Leases:      input.Delta.Txleases,
		Createables: input.Delta.Creatables,
	}

	input.BlockHeader.StateProofTracking = nil
	input.Delta.Hdr.StateProofTracking = nil
	input.Delta.Txleases = nil
	input.Delta.Creatables = nil

	savedMaps.LocalDeltas = map[string]map[uint64]types.StateDelta{}
	for i := 0; i < len(input.Payset); i++ {
		txID := crypto.GetTxID(input.Payset[i].Txn)

		savedMaps.LocalDeltas[txID] = input.Payset[i].EvalDelta.LocalDeltas
		input.Payset[i].EvalDelta.LocalDeltas = nil

		removeInnerLocalDeltas(&input.Payset[i].EvalDelta.InnerTxns, savedMaps.LocalDeltas)
	}

	start := time.Now()
	a.logger.Debug("Encoding block data")
	encodedInput := msgpack.Encode(input)

	a.logger.Debugf("Sending block data to websocket (size: %dkb)", len(encodedInput)/1000)
	err := wsutil.WriteServerBinary(a.conn, encodedInput)

	if err != nil {
		return input, err
	}

	a.logger.Debug("Waiting for response from websocket")
	encodedResponse, op, err := wsutil.ReadClientData(a.conn)

	if err != nil {
		return input, err
	}

	a.logger.Debug("Decoding response from websocket")
	var processedInput data.BlockData

	if op == ws.OpBinary {
		err = msgpack.Decode(encodedResponse, &processedInput)

		if err != nil {
			return input, nil
		}
	} else {
		return input, fmt.Errorf("unexpected op: %d", op)
	}

	a.logger.Infof("Data processed in %s", time.Since(start))

	// Restore all of the stuff we removed earlier for encoding purposes
	for i := 0; i < len(processedInput.Payset); i++ {
		txID := crypto.GetTxID(processedInput.Payset[i].Txn)

		processedInput.Payset[i].EvalDelta.LocalDeltas = savedMaps.LocalDeltas[txID]
		restoreInnerLocalDeltas(&processedInput.Payset[i].EvalDelta.InnerTxns, savedMaps.LocalDeltas)
	}
	processedInput.BlockHeader.StateProofTracking = savedMaps.Spt
	processedInput.Delta.Hdr.StateProofTracking = savedMaps.DeltaSpt
	processedInput.Delta.Creatables = savedMaps.Createables
	processedInput.Delta.Txleases = savedMaps.Leases
	return processedInput, err
}
