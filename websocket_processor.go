package websocket_processor

import (
	"context"
	_ "embed" // used to embed config
	"fmt"
	"net/http"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/algorand/conduit/conduit/data"
	"github.com/algorand/conduit/conduit/plugins"
	"github.com/algorand/conduit/conduit/plugins/processors"

	"github.com/algorand/go-algorand-sdk/v2/encoding/msgpack"
	"github.com/algorand/go-algorand-sdk/v2/types"

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
	EnableFilterEndpoint bool   `yaml:"enable-filter-endpoint"`
	EnableBlocksEndpoint bool   `yaml:"enable-blocks-endpoint"`
	EnableLogsEndpoint   bool   `yaml:"enable-logs-endpoint"`
	Host                 string `yaml:"host"`
	Port                 int    `yaml:"port"`
}

type WebsocketProcessor struct {
	logger           *log.Logger
	ctx              context.Context
	blockConnections map[uuid.UUID]*websocket.Conn
	logConnections   map[types.AppIndex]map[uuid.UUID]*websocket.Conn
	filterConn       *websocket.Conn
	responseChannel  chan data.BlockData
	cfg              ExporterConfig
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

func (a *WebsocketProcessor) setupReadEndpoint(w http.ResponseWriter, r *http.Request, connections map[uuid.UUID]*websocket.Conn) (uuid.UUID, *websocket.Conn, error) {
	u := websocket.NewUpgrader()
	u.CheckOrigin = func(r *http.Request) bool { return true }

	id := uuid.New()
	a.logger.Debug(id.String() + " connected")

	u.OnClose(func(c *websocket.Conn, err error) {
		delete(connections, id)
	})

	conn, err := u.Upgrade(w, r, nil)
	if err != nil {
		return id, conn, err
	}

	conn.SetReadDeadline(time.Time{})

	return id, conn, err
}

func (a *WebsocketProcessor) serve() {
	r := chi.NewRouter()

	r.Use(middleware.Logger)

	if a.cfg.EnableBlocksEndpoint {
		r.Get("/blocks", func(w http.ResponseWriter, r *http.Request) {
			id, conn, err := a.setupReadEndpoint(w, r, a.blockConnections)

			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}

			a.blockConnections[id] = conn
		})
	}

	if a.cfg.EnableLogsEndpoint {
		r.Get("/logs/{appID}", func(w http.ResponseWriter, r *http.Request) {
			appUint, err := strconv.ParseUint(chi.URLParam(r, "appID"), 10, 64)

			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			appID := types.AppIndex(appUint)

			if a.logConnections[appID] == nil {
				a.logConnections[appID] = map[uuid.UUID]*websocket.Conn{}
			}

			id, conn, err := a.setupReadEndpoint(w, r, a.logConnections[appID])

			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}

			a.logConnections[appID][id] = conn

		})
	}

	if a.cfg.EnableFilterEndpoint {
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

	a.blockConnections = map[uuid.UUID]*websocket.Conn{}
	a.logConnections = map[types.AppIndex]map[uuid.UUID]*websocket.Conn{}

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

	if !(a.cfg.EnableFilterEndpoint || a.cfg.EnableBlocksEndpoint || a.cfg.EnableLogsEndpoint) {
		return fmt.Errorf("at least of the endpoints must be enabled")
	}

	go a.serve()
	return nil
}

func (a *WebsocketProcessor) Close() error {
	for _, conn := range a.blockConnections {
		conn.Close()
	}

	for _, appID := range a.logConnections {
		for _, conn := range appID {
			conn.Close()
		}
	}

	return nil
}

func getInnerLogs(logs map[types.AppIndex][]string, itxns []types.SignedTxnWithAD) {
	for _, itxn := range itxns {
		if len(itxn.EvalDelta.Logs) > 0 {
			appID := itxn.Txn.ApplicationID
			if logs[appID] == nil {
				logs[appID] = []string{}
			}

			for _, log := range itxn.EvalDelta.Logs {
				logs[appID] = append(logs[appID], log)
			}
		}

		getInnerLogs(logs, itxn.EvalDelta.InnerTxns)
	}
}

// Process processes the input data
func (a *WebsocketProcessor) Process(input data.BlockData) (data.BlockData, error) {
	start := time.Now()

	a.logger.Debug("Encoding block data")
	encodedInput := msgpack.Encode(input)

	if a.cfg.EnableFilterEndpoint {
		for {
			if a.filterConn != nil {
				a.filterConn.WriteMessage(websocket.BinaryMessage, encodedInput)
				break
			}
		}
	}

	if a.cfg.EnableBlocksEndpoint {
		a.logger.Debugf("Sending block data to %d clients (size: %dkb)", len(a.blockConnections), len(encodedInput)/1000)
		for _, conn := range a.blockConnections {
			go func(conn *websocket.Conn) {
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				err := conn.WriteMessage(websocket.BinaryMessage, encodedInput)

				if err != nil {
					conn.Close()
				}
			}(conn)
		}
	}

	if a.cfg.EnableLogsEndpoint {
		logs := map[types.AppIndex][]string{}

		for _, p := range input.Payset {
			if len(p.EvalDelta.Logs) > 0 {
				appID := p.Txn.ApplicationID
				if logs[appID] == nil {
					logs[appID] = []string{}
				}

				for _, log := range p.EvalDelta.Logs {
					logs[appID] = append(logs[appID], log)
				}
			}

			getInnerLogs(logs, p.EvalDelta.InnerTxns)
		}

		for appID, conns := range a.logConnections {
			for _, conn := range conns {
				go func(conn *websocket.Conn) {
					conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					err := conn.WriteMessage(websocket.BinaryMessage, msgpack.Encode(logs[appID]))

					if err != nil {
						conn.Close()
					}
				}(conn)
			}
		}
	}

	var responseData data.BlockData

	if a.cfg.EnableFilterEndpoint && a.filterConn != nil {
		for responseData = range a.responseChannel {
			break
		}
	} else {
		responseData = input
	}

	a.logger.Infof("done in %s", time.Since(start))

	return responseData, nil
}
