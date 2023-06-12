package websocket_processor

import (
	"context"
	_ "embed" // used to embed config
	"fmt"
	"net"

	log "github.com/sirupsen/logrus"

	"github.com/algorand/conduit/conduit/data"
	"github.com/algorand/conduit/conduit/plugins"
	"github.com/algorand/conduit/conduit/plugins/processors"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"

	"github.com/algorand/go-algorand-sdk/v2/encoding/msgpack"
)

type Config struct {
	// <code>omit-group-transactions</code> configures the filter processor to return the matched transaction without its grouped transactions.
	IncludeGroupTransactions bool `yaml:"omit-group-transactions"`
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
func (a *WebsocketProcessor) Init(ctx context.Context, _ data.InitProvider, cfg plugins.PluginConfig, logger *log.Logger) error {
	a.logger = logger
	a.ctx = ctx

	listener, err := net.Listen("tcp", "localhost:8888")

	if err != nil {
		return err
	}

	conn, err := listener.Accept()

	if err != nil {
		return err
	}

	a.conn = conn

	return nil

}

func (a *WebsocketProcessor) Close() error {
	a.conn.Close()
	return nil
}

// Process processes the input data
func (a *WebsocketProcessor) Process(input data.BlockData) (data.BlockData, error) {
	a.logger.Debug("Sending block data to websocket")
	err := wsutil.WriteServerBinary(a.conn, msgpack.Encode(input))

	if err != nil {
		return input, err
	}

	a.logger.Debug("Waiting for response from websocket")
	data, op, err := wsutil.ReadClientData(a.conn)

	if err != nil {
		return input, err
	}

	a.logger.Debug("Decoded response from websocket")
	if op == ws.OpBinary {
		err = msgpack.Decode(data, &input)
	} else {
		return input, fmt.Errorf("unexpected op: %d", op)
	}

	a.logger.Debug("Websocket response decoded")

	// Encode BlockData to msgpack and send to websocket
	// Wait for response
	return input, err
}
