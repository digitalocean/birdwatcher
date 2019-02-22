package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"strings"

	"github.com/ecix/birdwatcher/bird"
	"github.com/ecix/birdwatcher/endpoints"
	"github.com/gorilla/handlers"

	"github.com/julienschmidt/httprouter"
)

//go:generate versionize
var VERSION = "1.11.2"

func isModuleEnabled(module string, modulesEnabled []string) bool {
	for _, enabled := range modulesEnabled {
		if enabled == module {
			return true
		}
	}

	return false
}

func makeRouter(config endpoints.ServerConfig) *httprouter.Router {
	whitelist := config.ModulesEnabled

	r := httprouter.New()
	if isModuleEnabled("status", whitelist) {
		r.GET("/version", endpoints.Version(VERSION))
		r.GET("/status", endpoints.Endpoint(endpoints.Status))
	}
	if isModuleEnabled("protocols", whitelist) {
		r.GET("/protocols", endpoints.Endpoint(endpoints.Protocols))
	}
	if isModuleEnabled("protocols_bgp", whitelist) {
		r.GET("/protocols/bgp", endpoints.Endpoint(endpoints.Bgp))
	}
	if isModuleEnabled("symbols", whitelist) {
		r.GET("/symbols", endpoints.Endpoint(endpoints.Symbols))
	}
	if isModuleEnabled("symbols_tables", whitelist) {
		r.GET("/symbols/tables", endpoints.Endpoint(endpoints.SymbolTables))
	}
	if isModuleEnabled("symbols_protocols", whitelist) {
		r.GET("/symbols/protocols", endpoints.Endpoint(endpoints.SymbolProtocols))
	}
	if isModuleEnabled("routes_protocol", whitelist) {
		r.GET("/routes/protocol/:protocol", endpoints.Endpoint(endpoints.ProtoRoutes))
	}
	if isModuleEnabled("routes_table", whitelist) {
		r.GET("/routes/table/:table", endpoints.Endpoint(endpoints.TableRoutes))
	}
	if isModuleEnabled("routes_count_protocol", whitelist) {
		r.GET("/routes/count/protocol/:protocol", endpoints.Endpoint(endpoints.ProtoCount))
	}
	if isModuleEnabled("routes_count_table", whitelist) {
		r.GET("/routes/count/table/:table", endpoints.Endpoint(endpoints.TableCount))
	}
	if isModuleEnabled("routes_filtered", whitelist) {
		r.GET("/routes/filtered/:protocol", endpoints.Endpoint(endpoints.RoutesFiltered))
	}
	if isModuleEnabled("routes_noexport", whitelist) {
		r.GET("/routes/noexport/:protocol", endpoints.Endpoint(endpoints.RoutesNoExport))
	}
	if isModuleEnabled("routes_prefixed", whitelist) {
		r.GET("/routes/prefix", endpoints.Endpoint(endpoints.RoutesPrefixed))
	}
	if isModuleEnabled("route_net", whitelist) {
		r.GET("/route/net/:net", endpoints.Endpoint(endpoints.RouteNet))
		r.GET("/route/net/:net/table/:table", endpoints.Endpoint(endpoints.RouteNetTable))
	}
	if isModuleEnabled("routes_peer", whitelist) {
		r.GET("/routes/peer", endpoints.Endpoint(endpoints.RoutesPeer))
	}
	if isModuleEnabled("routes_dump", whitelist) {
		r.GET("/routes/dump", endpoints.Endpoint(endpoints.RoutesDump))
	}
	return r
}

// Print service information like, listen address,
// access restrictions and configuration flags
func PrintServiceInfo(conf *Config, birdConf bird.BirdConfig) {
	// General Info
	log.Println("Starting Birdwatcher")
	log.Println("            Using:", birdConf.BirdCmd)
	log.Println("           Listen:", birdConf.Listen)
	log.Println("        Cache TTL:", birdConf.CacheTtl)

	// Endpoint Info
	if len(conf.Server.AllowFrom) == 0 {
		log.Println("        AllowFrom: ALL")
	} else {
		log.Println("        AllowFrom:", strings.Join(conf.Server.AllowFrom, ", "))
	}

	log.Println("   ModulesEnabled:")
	for _, m := range conf.Server.ModulesEnabled {
		log.Println("       -", m)
	}

	log.Println("   Per Peer Tables:", conf.Parser.PerPeerTables)
}

// MyLogger is our own log.Logger wrapper so we can customize it
type MyLogger struct {
	logger *log.Logger
}

// Write implements the Write method of io.Writer
func (m *MyLogger) Write(p []byte) (n int, err error) {
	m.logger.Print(string(p))
	return len(p), nil
}

func main() {
	bird6 := flag.Bool("6", false, "Use bird6 instead of bird")
	workerPoolSize := flag.Int("worker-pool-size", 8, "Number of go routines used to parse routing tables concurrently")
	configfile := flag.String("config", "etc/birdwatcher/birdwatcher.conf", "Configuration file location")
	flag.Parse()

	bird.WorkerPoolSize = *workerPoolSize

	conf, err := LoadConfigs([]string{*configfile})
	if err != nil {
		log.Fatal("Loading birdwatcher configuration failed:", err)
	}

	if conf.Server.EnableTLS {
		if len(conf.Server.Crt) == 0 || len(conf.Server.Key) == 0 {
			log.Fatalln("You have enabled TLS support. Please specify 'crt' and 'key' in birdwatcher config file.")
		}
	}

	endpoints.VERSION = VERSION
	bird.InstallRateLimitReset()

	// Get config according to flags
	birdConf := conf.Bird
	if *bird6 {
		birdConf = conf.Bird6
		bird.IPVersion = "6"
	}

	PrintServiceInfo(conf, birdConf)

	// Configuration
	bird.ClientConf = birdConf
	bird.StatusConf = conf.Status
	bird.RateLimitConf.Conf = conf.Ratelimit
	bird.ParserConf = conf.Parser
	endpoints.Conf = conf.Server

	// Make server
	r := makeRouter(conf.Server)

	// Set up our own custom log.Logger
	// Use this weird golang format to imitate log.Logger's timestamp in log.Prefix()
	ts := time.Now().Format("2006/01/02 15:04:05")
	// set log prefix timestamp to our own custom prefix
	log.SetPrefix(ts)
	myquerylog := log.New(os.Stdout, fmt.Sprintf("%s %s: ", ts, "QUERY"), 0)
	mylogger := &MyLogger{myquerylog}

	if conf.Server.EnableTLS {
		if len(conf.Server.Crt) == 0 || len(conf.Server.Key) == 0 {
			log.Fatalln("You have enabled TLS support but not specified both a .crt and a .key file in the config.")
		}
		log.Fatal(http.ListenAndServeTLS(birdConf.Listen, conf.Server.Crt, conf.Server.Key, handlers.LoggingHandler(mylogger, r)))
	} else {
		log.Fatal(http.ListenAndServe(birdConf.Listen, handlers.LoggingHandler(mylogger, r)))
	}
}
