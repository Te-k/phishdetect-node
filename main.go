// PhishDetect
// Copyright (c) 2018-2019 Claudio Guarnieri.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	pongo "github.com/flosch/pongo2"
	"github.com/gobuffalo/packr"
	"github.com/gorilla/mux"
	"github.com/mattn/go-colorable"
	"github.com/phishdetect/phishdetect"
	"github.com/phishdetect/phishdetect/brand"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

const base64Regex string = "(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=|[A-Za-z0-9+/]{4})"
const sha256Regex string = "[a-fA-F0-9]{64}"

var (
	host         string
	portNumber   string
	apiVersion   string
	safeBrowsing string
	brandsPath   string
	yaraPath     string

	disableAPI      bool
	disableGUI      bool
	disableAnalysis bool

	operatorContacts string

	db *Database

	templatesBox packr.Box
	staticBox    packr.Box
	tmplSet      *pongo.TemplateSet

	customBrands []*brand.Brand
)

func init() {
	debug := flag.Bool("debug", false, "Enable debug logging")

	flag.StringVar(&host, "host", "127.0.0.1", "Specify the host to bind the service on")
	flag.StringVar(&portNumber, "port", "7856", "Specify which port number to bind the service on")
	flag.StringVar(&apiVersion, "api-version", "1.37", "Specify which Docker API version to use")
	flag.StringVar(&safeBrowsing, "safebrowsing", "", "Specify a file path containing your Google SafeBrowsing API key (default disabled)")
	flag.StringVar(&brandsPath, "brands", "", "Specify a folder containing YAML files with Brand specifications")
	flag.StringVar(&yaraPath, "yara", "", "Specify a path to a file or folder contaning Yara rules")
	flag.BoolVar(&disableAPI, "disable-api", false, "Disable the API routes")
	flag.BoolVar(&disableGUI, "disable-web", false, "Disable the Web GUI")
	flag.BoolVar(&disableAnalysis, "disable-analysis", false, "Disable the ability to analyze links and pages")
	flag.StringVar(&operatorContacts, "contacts", "", "Specify a link to information or contacts details to be provided to your users")
	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
	}
	log.SetFormatter(&log.TextFormatter{ForceColors: true})
	log.SetOutput(colorable.NewColorableStdout())

	// Initiate connection to database.
	var err error
	db, err = NewDatabase()
	if err != nil {
		log.Fatal("Failed connection to database: ", err.Error())
		return
	}

	if safeBrowsing != "" {
		if _, err := os.Stat(safeBrowsing); err == nil {
			buf, _ := ioutil.ReadFile(safeBrowsing)
			key := string(buf)
			if key != "" {
				phishdetect.SafeBrowsingKey = key
			}
		} else {
			log.Warning("The specified Google SafeBrowsing API key file does not exist: check disabled")
		}
	}

	if yaraPath != "" {
		if _, err := os.Stat(yaraPath); err == nil {
			phishdetect.YaraRulesPath = yaraPath
		} else {
			log.Warning("The specified path to the Yara rules does not exist")
		}
	}

	templatesBox = packr.NewBox("templates")
	staticBox = packr.NewBox("static")

	tmplSet = pongo.NewSet("templates", packrBoxLoader{&templatesBox})

	customBrands = compileBrands()
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug(r.RemoteAddr, " ", r.Method, " ", r.RequestURI)
		next.ServeHTTP(w, r)
	})
}

func main() {
	fs := http.FileServer(staticBox)

	router := mux.NewRouter()
	router.StrictSlash(true)
	router.Use(loggingMiddleware)

	// Graphical interface routes.
	if disableGUI == false {
		router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))
		router.HandleFunc("/", guiIndex)
		router.HandleFunc("/contacts/", guiContacts)
		router.HandleFunc("/check/", guiCheck)
		router.HandleFunc("/link/analyze/", guiLinkAnalyze).Methods("POST")
		router.HandleFunc(fmt.Sprintf("/link/{url:%s}", base64Regex), guiLinkCheck).Methods("GET", "POST")
		router.HandleFunc(fmt.Sprintf("/report/{url:%s}", base64Regex), guiReport).Methods("GET")
		router.HandleFunc(fmt.Sprintf("/review/{ioc:%s}", sha256Regex), guiReview).Methods("GET")
	}

	// REST API routes.
	if disableAPI == false {
		router.HandleFunc("/api/config/", apiConfig).Methods("GET")
		router.HandleFunc("/api/analyze/link/", apiAnalyzeLink).Methods("POST")
		router.HandleFunc("/api/analyze/domain/", apiAnalyzeDomain).Methods("POST")
		router.HandleFunc("/api/analyze/html/", apiAnalyzeHTML).Methods("POST")
		router.HandleFunc("/api/indicators/fetch/", apiIndicatorsFetch).Methods("GET")
		router.HandleFunc("/api/indicators/fetch/recent/", apiIndicatorsFetchRecent).Methods("GET")
		router.HandleFunc("/api/indicators/fetch/all/", apiIndicatorsFetchAll).Methods("GET")
		router.HandleFunc("/api/indicators/add/", apiIndicatorsAdd).Methods("POST")
		router.HandleFunc("/api/indicators/details/", apiIndicatorsDetails).Methods("POST")
		router.HandleFunc("/api/events/fetch/", apiEventsFetch).Methods("POST")
		router.HandleFunc("/api/events/add/", apiEventsAdd).Methods("POST")
		router.HandleFunc("/api/raw/fetch/", apiRawFetch).Methods("POST")
		router.HandleFunc("/api/raw/add/", apiRawAdd).Methods("POST")
		router.HandleFunc("/api/raw/details/", apiRawDetails).Methods("POST")
	}

	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Warning("File not found: ", r.RequestURI)
		errorWithJSON(w, "File not found", http.StatusNotFound, nil)
	})

	hostPort := fmt.Sprintf("%s:%s", host, portNumber)
	srv := &http.Server{
		Handler:      router,
		Addr:         hostPort,
		WriteTimeout: 2 * time.Minute,
		ReadTimeout:  2 * time.Minute,
	}

	log.Info("Starting PhishDetect Node on ", hostPort, " and waiting for requests...")

	log.Fatal(srv.ListenAndServe())
}
