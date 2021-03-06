// PhishDetect
// Copyright (c) 2018-2020 Claudio Guarnieri.
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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"gopkg.in/go-playground/validator.v9"
)

// IndicatorTypeEmail contains the name of the type of indicator as stored
// in the database.
const IndicatorTypeEmail = "email"
// IndicatorTypeDomain contains the name of the type of indicator as stored
// in the database.
const IndicatorTypeDomain = "domain"
// IndicatorsGroupEmails contains the key name for the type of indicators
// as returned when the lists is fetched by clients.
const IndicatorsGroupEmails = "emails"
// IndicatorsGroupDomains contains the key name for the type of indicators
// as returned when the lists is fetched by clients.
const IndicatorsGroupDomains = "domains"

// RequestIndicatorsAdd contains the fields submitted by an administrator or
// submitter when requesting to add new indicators to the database.
type RequestIndicatorsAdd struct {
	Type       string   `json:"type"`
	Indicators []string `json:"indicators"`
	Tags       []string `json:"tags"`
	Enabled    bool     `json:"enabled"`
}

func cleanIndicator(indicator string) string {
	indicator = strings.TrimSpace(indicator)
	indicator = strings.ToLower(indicator)
	indicator = strings.Replace(indicator, "[@]", "@", -1)
	indicator = strings.Replace(indicator, "[.]", ".", -1)
	indicator = strings.Replace(indicator, "\\@", "@", -1)
	indicator = strings.Replace(indicator, "\\.", ".", -1)

	if !strings.Contains(indicator, "@") && strings.HasPrefix(indicator, "www.") {
		indicator = indicator[4:]
	}

	return indicator
}

func detectIndicatorType(indicator string) (string, error) {
	validate := validator.New()
	if validate.Var(indicator, "email") == nil {
		return IndicatorTypeEmail, nil
	}

	if validate.Var(indicator, "fqdn") == nil {
		return IndicatorTypeDomain, nil
	}

	return "", errors.New("Invalid indicator type")
}

func prepareIndicators(iocs []Indicator) map[string][]string {
	// We loop through the list of indicators and generate the response.
	emails := []string{}
	domains := []string{}
	for _, ioc := range iocs {
		switch ioc.Type {
		case IndicatorTypeEmail:
			emails = append(emails, ioc.Hashed)
		case IndicatorTypeDomain:
			domains = append(domains, ioc.Hashed)
		}
	}
	// We assemble the response.
	indicators := map[string][]string{
		IndicatorsGroupEmails:  emails,
		IndicatorsGroupDomains: domains,
	}

	return indicators
}

func apiIndicatorsFetch(w http.ResponseWriter, r *http.Request) {
	// We get the indicators from the DB.
	iocs, err := db.GetIndicators(IndicatorsLimit6Months, true)
	if err != nil {
		errorWithJSON(w, ErrorMsgIndicatorsFetchFailed, http.StatusInternalServerError, err)
		return
	}

	indicators := prepareIndicators(iocs)
	responseWithJSON(w, indicators)
}

func apiIndicatorsFetchRecent(w http.ResponseWriter, r *http.Request) {
	iocs, err := db.GetIndicators(IndicatorsLimit24Hours, true)
	if err != nil {
		errorWithJSON(w, ErrorMsgIndicatorsFetchFailed, http.StatusInternalServerError, err)
		return
	}

	indicators := prepareIndicators(iocs)
	responseWithJSON(w, indicators)
}

func apiIndicatorsFetchAll(w http.ResponseWriter, r *http.Request) {
	iocs, err := db.GetIndicators(IndicatorsLimitAll, true)
	if err != nil {
		errorWithJSON(w, ErrorMsgIndicatorsFetchFailed, http.StatusInternalServerError, err)
		return
	}

	indicators := prepareIndicators(iocs)
	responseWithJSON(w, indicators)
}

func apiIndicatorsFetchDisabled(w http.ResponseWriter, r *http.Request) {
	iocs, err := db.GetIndicators(IndicatorsLimitAll, false)
	if err != nil {
		errorWithJSON(w, ErrorMsgIndicatorsFetchFailed, http.StatusInternalServerError, err)
		return
	}

	responseWithJSON(w, iocs)
}

func apiIndicatorsAdd(w http.ResponseWriter, r *http.Request) {
	// We decode the request to an RequestIndicatorsAdd.
	decoder := json.NewDecoder(r.Body)
	var req RequestIndicatorsAdd
	err := decoder.Decode(&req)
	if err != nil {
		errorWithJSON(w, ErrorMsgParseRequestFailed, http.StatusBadRequest, err)
		return
	}

	user, err := db.GetUserByKey(getAPIKeyFromRequest(r))
	if err != nil {
		errorWithJSON(w, ErrorMsgNotAuthorized, http.StatusUnauthorized, nil)
		return
	}

	// We loop through the submitted indicators and try to add them to the DB.
	addedCounter := 0
	for _, indicator := range req.Indicators {
		var hashed string
		// Check if we received an already hashed IOC.
		if validateSHA256(indicator) {
			// If we do, the indicator is already the hashed version.
			hashed = indicator
			// And the original indicator value shall be blank.
			indicator = ""
		} else {
			// Otherwise, we first clean the indicator...
			indicator = cleanIndicator(indicator)
			// ... then we hash the original indicator.
			hashed = encodeSHA256(indicator)
		}

		// We try to automatically determine the indicator type.
		// If we can't, we skip this indicator as it might be of an unsupported
		// format.
		indicatorType, err := detectIndicatorType(indicator)
		if err != nil {
			continue
		}

		ioc := Indicator{
			Type:     indicatorType,
			Original: indicator,
			Hashed:   hashed,
			Tags:     req.Tags,
			Datetime: time.Now().UTC(),
			Owner:    user.Name,
			Enabled:  req.Enabled,
		}

		err = db.AddIndicator(ioc)
		if err != nil {
			log.Warning("Failed to add indicator to database: ", err.Error())
			continue
		}
		// If the addition was successful, we increase the counter.
		addedCounter++
	}

	response := map[string]interface{}{
		"msg":     fmt.Sprintf("Added %d indicators", addedCounter),
		"counter": addedCounter,
	}

	responseWithJSON(w, response)
}

func apiIndicatorsToggle(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var indicators []string
	err := decoder.Decode(&indicators)
	if err != nil {
		errorWithJSON(w, ErrorMsgParseRequestFailed, http.StatusBadRequest, err)
		return
	}

	toggledCounter := 0
	for _, indicator := range indicators {
		if !validateSHA256(indicator) {
			continue
		}

		ioc, err := db.GetIndicatorByHash(indicator)
		if err != nil {
			continue
		}

		ioc.Enabled = !ioc.Enabled
		err = db.UpdateIndicator(ioc)
		if err != nil {
			log.Warning("Failed to update indicator: ", err.Error())
			continue
		}

		toggledCounter++
	}

	response := map[string]interface{}{
		"msg":     fmt.Sprintf("Toggled %d indicators", toggledCounter),
		"counter": toggledCounter,
	}

	responseWithJSON(w, response)
}

func apiIndicatorsDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	ioc, err := db.GetIndicatorByHash(vars["ioc"])
	if err != nil {
		errorWithJSON(w, "Failed to fetch indicator from database", http.StatusInternalServerError, err)
	}

	responseWithJSON(w, ioc)
}
