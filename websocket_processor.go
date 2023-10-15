package websocket_processor

import (
	"context"
	_ "embed" // used to embed config
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/algorand/conduit/conduit/data"
	"github.com/algorand/conduit/conduit/plugins"
	"github.com/algorand/conduit/conduit/plugins/processors"

	"github.com/algorand/go-algorand-sdk/v2/encoding/msgpack"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/lesismal/nbio/nbhttp/websocket"
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
	logger      *log.Logger
	ctx         context.Context
	connections map[uuid.UUID]*websocket.Conn
}

// Metadata returns metadata
func (a *WebsocketProcessor) Metadata() plugins.Metadata {
	return plugins.Metadata{
		Name:         PluginName,
		Description:  "Pass block data any number of websocket connections",
		Deprecated:   false,
		SampleConfig: "",
	}
}

// Config returns the config
func (a *WebsocketProcessor) Config() string {
	return ""
}

func (a *WebsocketProcessor) Serve() {
	r := chi.NewRouter()

	r.Use(middleware.Logger)

	r.Get("/read", func(w http.ResponseWriter, r *http.Request) {
		u := websocket.NewUpgrader()
		u.CheckOrigin = func(r *http.Request) bool { return true }

		id := uuid.New()
		a.logger.Debug(id.String() + " connected")

		u.OnClose(func(c *websocket.Conn, err error) {
			delete(a.connections, id)
		})

		conn, err := u.Upgrade(w, r, nil)

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		conn.SetReadDeadline(time.Time{})

		a.connections[id] = conn
	})

	http.ListenAndServe("localhost:8888", r)
}

// Init initializes the filter processor
func (a *WebsocketProcessor) Init(ctx context.Context, _ data.InitProvider, _ plugins.PluginConfig, logger *log.Logger) error {
	a.logger = logger
	a.ctx = ctx
	a.logger.Debug("Initializing websocket processor")

	a.connections = map[uuid.UUID]*websocket.Conn{}

	go a.Serve()
	return nil
}

func (a *WebsocketProcessor) Close() error {
	for _, conn := range a.connections {
		conn.Close()
	}

	return nil
}

// Process processes the input data
func (a *WebsocketProcessor) Process(input data.BlockData) (data.BlockData, error) {
	start := time.Now()

	a.logger.Debug("Encoding block data")
	encodedInput := msgpack.Encode(input)

	a.logger.Debugf("Sending block data to read all %d clients (size: %dkb)", len(a.connections), len(encodedInput)/1000)
	for id, conn := range a.connections {
		a.logger.Debug("Sending to " + id.String())

		go func(conn *websocket.Conn) {
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := conn.WriteMessage(websocket.BinaryMessage, encodedInput)

			if err != nil {
				conn.Close()
			}
		}(conn)
	}

	a.logger.Infof("done in %s", time.Since(start))

	return input, nil
}
