package websocket_processor

import (
	"context"
	_ "embed" // used to embed config
	"fmt"
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

type ExporterConfig struct {
	EnableFilter bool   `yaml:"enable-filter"`
	EnableRead   bool   `yaml:"enable-read"`
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
}

type WebsocketProcessor struct {
	logger          *log.Logger
	ctx             context.Context
	connections     map[uuid.UUID]*websocket.Conn
	filterConn      *websocket.Conn
	responseChannel chan data.BlockData
	cfg             ExporterConfig
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

func (a *WebsocketProcessor) serve() {
	r := chi.NewRouter()

	r.Use(middleware.Logger)

	if a.cfg.EnableRead {
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
	}

	if a.cfg.EnableFilter {
		r.Get("/filter", func(w http.ResponseWriter, r *http.Request) {
			u := websocket.NewUpgrader()
			u.CheckOrigin = func(r *http.Request) bool { return true }

			u.OnOpen(func(c *websocket.Conn) {
				a.filterConn = c
				a.responseChannel = make(chan data.BlockData, 1)
			})

			u.OnClose(func(c *websocket.Conn, err error) {
				close(a.responseChannel)
				a.filterConn = nil
			})

			u.OnMessage(func(c *websocket.Conn, t websocket.MessageType, msg []byte) {
				var responseData data.BlockData
				if t == websocket.BinaryMessage {
					msgpack.Decode(msg, &responseData)
					a.responseChannel <- responseData
				}
			})

			conn, err := u.Upgrade(w, r, nil)

			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			conn.SetReadDeadline(time.Time{})
		})
	}

	a.logger.Infof("Starting server on %s:%d", a.cfg.Host, a.cfg.Port)
	http.ListenAndServe(fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port), r)
}

// Init initializes the filter processor
func (a *WebsocketProcessor) Init(ctx context.Context, _ data.InitProvider, cfg plugins.PluginConfig, logger *log.Logger) error {
	a.logger = logger
	a.ctx = ctx
	a.logger.Debug("Initializing websocket processor")

	a.connections = map[uuid.UUID]*websocket.Conn{}

	err := cfg.UnmarshalConfig(&a.cfg)

	if err != nil {
		return fmt.Errorf("connect failure in unmarshalConfig: %v", err)
	}

	if a.cfg.Host == "" {
		a.cfg.Host = "localhost"
	}

	if a.cfg.Port == 0 {
		a.cfg.Port = 8888
	}

	a.logger.Debug(a.cfg)

	if !(a.cfg.EnableFilter || a.cfg.EnableRead) {
		return fmt.Errorf("either /filter or /read endpoint must be enabled")
	}

	go a.serve()
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

	if a.cfg.EnableFilter {
		for {
			if a.filterConn != nil {
				a.filterConn.WriteMessage(websocket.BinaryMessage, encodedInput)
				break
			}
		}
	}

	if a.cfg.EnableRead {
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
	}

	var responseData data.BlockData

	if a.cfg.EnableFilter && a.filterConn != nil {
		for responseData = range a.responseChannel {
			a.logger.Debug(string(responseData.BlockHeader.TimeStamp))
			break
		}
	} else {
		responseData = input
	}

	a.logger.Infof("done in %s", time.Since(start))

	return responseData, nil
}
