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
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

const urlRegex string = "(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=|[A-Za-z0-9+/]{4})"

var (
	portNumber   string
	apiVersion   string
	safeBrowsing string

	disableAPI      bool
	disableWeb      bool
	disableAnalysis bool

	db *Database

	templatesBox packr.Box
	staticBox    packr.Box

	tmplIndex    *pongo.Template
	tmplError    *pongo.Template
	tmplSubmit   *pongo.Template
	tmplCheck    *pongo.Template
	tmplRedirect *pongo.Template
	tmplWarning  *pongo.Template
)

func init() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.StringVar(&portNumber, "port", "7856", "Specify which port number to bind the service on")
	flag.StringVar(&apiVersion, "api-version", "1.37", "Specify which Docker API version to use")
	flag.StringVar(&safeBrowsing, "safebrowsing", "", "Specify a file path containing your Google SafeBrowsing API key (default disabled)")
	flag.BoolVar(&disableAPI, "disable-api", false, "Disable the API routes")
	flag.BoolVar(&disableWeb, "disable-web", false, "Disable the Web GUI")
	flag.BoolVar(&disableAnalysis, "disable-analysis", false, "Disable the ability to analyze links and pages")
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
			log.Warning("The specified Google SafeBrowsing API key file does not exist. Check disabled.")
		}
	}

	templatesBox = packr.NewBox("templates")
	staticBox = packr.NewBox("static")

	strIndex, _ := templatesBox.FindString("index.html")
	tmplIndex = pongo.Must(pongo.FromString(strIndex))

	strError, _ := templatesBox.FindString("error.html")
	tmplError = pongo.Must(pongo.FromString(strError))

	strSubmit, _ := templatesBox.FindString("submit.html")
	tmplSubmit = pongo.Must(pongo.FromString(strSubmit))

	strCheck, _ := templatesBox.FindString("check.html")
	tmplCheck = pongo.Must(pongo.FromString(strCheck))

	strRedirect, _ := templatesBox.FindString("redirect.html")
	tmplRedirect = pongo.Must(pongo.FromString(strRedirect))

	strWarning, _ := templatesBox.FindString("warning.html")
	tmplWarning = pongo.Must(pongo.FromString(strWarning))
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug(r.RemoteAddr, " ", r.Method, " ", r.RequestURI)
		next.ServeHTTP(w, r)
	})
}

func errorPage(w http.ResponseWriter, message string) {
	err := tmplError.ExecuteWriter(pongo.Context{
		"message": message,
	}, w)
	if err != nil {
		log.Error(err)
		http.Error(w, "Some unexpected error occurred! :-(", http.StatusInternalServerError)
	}
	return
}

func main() {
	fs := http.FileServer(staticBox)

	router := mux.NewRouter()
	router.StrictSlash(true)
	router.Use(loggingMiddleware)

	// Graphical interface routes.
	if disableWeb == false {
		router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))
		router.HandleFunc("/", interfaceIndex)
		router.HandleFunc("/check/", interfaceCheck)
		router.HandleFunc(fmt.Sprintf("/check/{url:%s}", urlRegex), interfaceCheck).Methods("GET", "POST")
		router.HandleFunc("/analyze/", interfaceAnalyze).Methods("POST")
	}

	// REST API routes.
	if disableAPI == false {
		router.HandleFunc("/api/analyze/link/", apiAnalyzeLink).Methods("POST")
		router.HandleFunc("/api/analyze/domain/", apiAnalyzeDomain).Methods("POST")
		router.HandleFunc("/api/analyze/html/", apiAnalyzeHTML).Methods("POST")
		router.HandleFunc("/api/indicators/fetch/", apiIndicatorsFetch).Methods("GET")
		router.HandleFunc("/api/indicators/add/", apiIndicatorsAdd).Methods("POST")
		router.HandleFunc("/api/indicators/details/", apiIndicatorsDetails).Methods("POST")
		router.HandleFunc("/api/events/fetch/", apiEventsFetch).Methods("POST")
		router.HandleFunc("/api/events/add/", apiEventsAdd).Methods("POST")
		router.HandleFunc("/api/raw/add/", apiRawAdd).Methods("POST")
	}

	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Warning("File not found: ", r.RequestURI)
		errorWithJSON(w, "File not found", http.StatusNotFound, nil)
	})

	hostPort := fmt.Sprintf("127.0.0.1:%s", portNumber)
	srv := &http.Server{
		Handler:      router,
		Addr:         hostPort,
		WriteTimeout: 2 * time.Minute,
		ReadTimeout:  2 * time.Minute,
	}

	log.Info("Starting PhishDetect Node on ", hostPort, " and waiting for requests...")

	log.Fatal(srv.ListenAndServe())
}
