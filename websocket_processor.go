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

	"github.com/algorand/go-algorand-sdk/v2/encoding/msgpack"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

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
		Description:  "Pass block data to a websocket server and receive back the processed data",
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

// Process processes the input data
func (a *WebsocketProcessor) Process(input data.BlockData) (data.BlockData, error) {
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
	var output data.BlockData

	if op == ws.OpBinary {
		err = msgpack.Decode(encodedResponse, &output)

		if err != nil {
			return input, nil
		}
	} else {
		return input, fmt.Errorf("unexpected op: %d", op)
	}

	a.logger.Infof("Data processed in %s", time.Since(start))

	return output, err
}
