package main

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/badoux/goscraper"
	"github.com/go-chi/chi"

	"log"
	"net/http"
	"os/exec"
	"reflect"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

var db *sql.DB
var myClient = &http.Client{Timeout: 10 * time.Second}

func catch(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func init() {
	var err error

	db, err = sql.Open("postgres",
		"postgresql://maxroach@localhost:26257/hosting?ssl=true&sslmode=require&sslrootcert=certs/ca.crt&sslkey=certs/client.maxroach.key&sslcert=certs/client.maxroach.crt")

	catch(err)
}

func getJson(url string, target interface{}) error {
	r, err := myClient.Get(url)

	if err != nil {
		return err
	}

	defer r.Body.Close()
	//dataInBytes, err := ioutil.ReadAll(r.Body)
	//fmt.Println(string(dataInBytes))

	return json.NewDecoder(r.Body).Decode(target)
}

// respondwithJSON write json response format
func respondwithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func main() {
	fmt.Println("Starting the application...")

	router := chi.NewRouter()

	router.Get("/consultDomain", consultDomain)
	router.Get("/items", items)

	http.ListenAndServe(":3000", router)
}

func consultDomain(w http.ResponseWriter, r *http.Request) {
	val := r.FormValue("domain")
	endpointAnalyze := strings.Replace("https://api.ssllabs.com/api/v3/analyze?host=myDomain", "myDomain", val, -1)
	//fmt.Println("Analyze URL: ", endpointAnalyze)
	myHost := new(Host)
	getJson(endpointAnalyze, myHost)

	myDominio := new(Dominio)

	var myServers = make([]Server, len(myHost.Endpoints))
	for i := 0; i < len(myServers); i++ {
		// myServer := new(Server)
		myServers[i].Address = myHost.Endpoints[i].IpAddress
		myServers[i].SslGrade = myHost.Endpoints[i].Grade
		//myServers = append(myServers, myServer)
	}

	if len(myServers) > 0 {
		for i := 0; i < len(myServers); i++ {
			myResponse := getConutryAnsOwner(myServers[i].Address)
			myServers[i].Country = myResponse.Country
			myServers[i].Owner = myResponse.Owner
		}
	}

	domainPage := getTitleAndIcon(val)
	myDominio.Host = val
	myDominio.Logo = domainPage.Logo
	myDominio.Title = domainPage.Title
	myDominio.Servers = myServers

	if len(myServers) > 0 {
		myDominio.SslGrade = myServers[0].SslGrade
	} else {
		myDominio.IsDown = true
	}

	if existDomainRegistered(val) {
		updateHost(myDominio)
	} else {
		registerHost(myDominio)
	}

	respondwithJSON(w, http.StatusOK, myDominio)
}

func items(w http.ResponseWriter, r *http.Request) {
	resultQuery := allDomains()
	respondwithJSON(w, http.StatusOK, resultQuery)
}

func getConutryAnsOwner(ip string) WhosIpResponse {
	// Execute command
	cmd, err := exec.Command("cmd", "/C", "whois", ip).Output()

	var whosIpResponse WhosIpResponse
	whosIpResponseType := reflect.TypeOf(whosIpResponse)
	whosIpResponsePointer := reflect.New(whosIpResponseType)
	whosIpResponseValue := whosIpResponsePointer.Elem()
	whosIpResponseInterface := whosIpResponseValue.Interface()
	response := whosIpResponseInterface.(WhosIpResponse)

	if err != nil {
		fmt.Println("Error: ", err)
	} else {
		resultCommand := string(cmd)
		startCountry := strings.Index(resultCommand, "Registrant Country:")
		startOwner := strings.Index(resultCommand, "Registrant Organization:")

		startCountry += 20
		startOwner += 25

		endCountryIndex := int(startCountry + 2)
		endOwnerIndex := int(startOwner + 17)

		countryByte := []byte(resultCommand[startCountry:endCountryIndex])
		ownerByte := []byte(resultCommand[startOwner:endOwnerIndex])

		temp_owner := strings.Split(string(ownerByte), "\r")

		response.Country = string(countryByte)
		response.Owner = temp_owner[0]

		fmt.Println("Country value: ", response.Country)
		fmt.Println("Owner value: ", response.Owner)
	}

	return response
}

func getTitleAndIcon(myDominio string) DomainPage {
	domain := strings.Replace("https://www.my_domain/", "my_domain", myDominio, -1)
	s, err := goscraper.Scrape(domain, 10)

	var domainPage DomainPage
	domainPageType := reflect.TypeOf(domainPage)
	domainPagePointer := reflect.New(domainPageType)
	domainPageValue := domainPagePointer.Elem()
	domainPageInterface := domainPageValue.Interface()
	response := domainPageInterface.(DomainPage)

	if err != nil {
		log.Fatal(err)
	} else {
		fmt.Println("Shortcut Icon:", s.Preview.ShortcutIcon)
		fmt.Println("Apple Touch Icon:", s.Preview.AppleTouchIcon)

		if len(s.Preview.ShortcutIcon) > 0 {
			response.Logo = s.Preview.ShortcutIcon
		} else {
			response.Logo = s.Preview.AppleTouchIcon
		}

		response.Title = s.Preview.Title
	}

	return response
}

func existDomainRegistered(my_domain string) bool {
	var totalRows int
	row := db.QueryRow("SELECT COUNT(*) AS totalRows FROM hosting.dominio WHERE host = $1", my_domain)
	err := row.Scan(&totalRows)

	if err != nil {
		log.Fatal(err)
	}

	return totalRows > 0
}

func registerHost(my_domain *Dominio) {
	var domain_id int

	err := db.QueryRow(
		"INSERT INTO hosting.dominio(host, servers_changed, ssl_grade, previous_ssl_grade, logo, title_page, is_down) VALUES($1, $2, $3, $4, $5, $6, $7) RETURNING domain_id",
		my_domain.Host, my_domain.ServersChanged, my_domain.SslGrade, my_domain.PreviousSslGrade, my_domain.Logo, my_domain.Title, my_domain.IsDown).Scan(&domain_id)

	if err != nil {
		log.Fatal(err)
	} else {
		fmt.Println("Domain id: ", domain_id)
	}

	registerServers(domain_id, my_domain.Servers)
}

func updateHost(my_domain *Dominio) {
	tempora_servers := []Server{}

	rows, err := db.Query("SELECT ser.address, ser.ssl_grade, ser.country, ser.owner FROM hosting.servidor ser INNER JOIN hosting.dominio dom ON (ser.domain_id = dom.domain_id) WHERE dom.host = $1", my_domain.Host)
	catch(err)

	defer rows.Close()

	for rows.Next() {
		data := Server{}

		er := rows.Scan(&data.Address, &data.SslGrade, &data.Country, &data.Owner)

		if er != nil {
			log.Fatal(err)
		}

		tempora_servers = append(tempora_servers, data)
	}

	var serversChanged bool = false

	for i := 0; i < len(my_domain.Servers); i++ {
		var findServer bool = false

		for j := 0; j < len(tempora_servers); j++ {
			if my_domain.Servers[i].Address == tempora_servers[j].Address {
				findServer = true

				if my_domain.Servers[i].SslGrade != tempora_servers[j].SslGrade {
					serversChanged = true
				}

				if my_domain.Servers[i].Country != tempora_servers[j].Country {
					serversChanged = true
				}

				if my_domain.Servers[i].Owner != tempora_servers[j].Owner {
					serversChanged = true
				}
			}
		}

		if findServer == false {
			serversChanged = true
		}
	}

	var domain_id int
	var ssl_grade string

	row := db.QueryRow("SELECT domain_id, ssl_grade FROM hosting.dominio WHERE host = $1", my_domain.Host)

	error := row.Scan(&domain_id, &ssl_grade)

	if error != nil {
		log.Fatal(error)
	}

	my_domain.ServersChanged = serversChanged
	my_domain.PreviousSslGrade = ssl_grade

	_, err = db.Exec("UPDATE hosting.dominio SET servers_changed=$1, ssl_grade=$2, previous_ssl_grade=$3, logo=$4, title_page=$5, is_down=$6 WHERE host=$7", my_domain.ServersChanged, my_domain.SslGrade, my_domain.PreviousSslGrade, my_domain.Logo, my_domain.Title, my_domain.IsDown, my_domain.Host)

	catch(err)

	if serversChanged || my_domain.IsDown {
		deleteServers(domain_id)

		if len(my_domain.Servers) > 0 {
			registerServers(domain_id, my_domain.Servers)
		}
	}
}

func registerServers(domain_id int, my_servers []Server) {
	for i := 0; i < len(my_servers); i++ {
		var server_id int

		err := db.QueryRow(
			"INSERT INTO hosting.servidor(address, ssl_grade, country, owner, domain_id) VALUES($1, $2, $3, $4, $5) RETURNING server_id",
			my_servers[i].Address, my_servers[i].SslGrade, my_servers[i].Country, my_servers[i].Owner, domain_id).Scan(&server_id)

		if err != nil {
			log.Fatal(err)
		} else {
			fmt.Println("Server id: ", server_id)
		}
	}
}

func deleteServers(domain_id int) {
	_, err := db.Exec("DELETE FROM hosting.servidor WHERE domain_id=$1", domain_id)
	catch(err)
}

func allDomains() []Dominio {
	temporal_domains := []Dominio{}

	rows, err := db.Query("SELECT host, servers_changed, ssl_grade, previous_ssl_grade, logo, title_page, is_down FROM hosting.dominio")
	catch(err)

	defer rows.Close()

	for rows.Next() {
		data := Dominio{}

		er := rows.Scan(&data.Host, &data.ServersChanged, &data.SslGrade, &data.PreviousSslGrade, &data.Logo, &data.Title, &data.IsDown)

		if er != nil {
			log.Fatal(err)
		}

		temporal_domains = append(temporal_domains, data)
	}

	return temporal_domains
}

type Endpoint struct {
	IpAddress         string
	ServerName        string
	StatusMessage     string
	Grade             string `json:"grade"`
	GradeTrustIgnored string
	HasWarnings       string
	IsExceptional     string
	Progress          string
	Duration          string
	Delegation        string
}

type Host struct {
	HostName        string
	Port            string
	Protocol        string
	IsPublic        string
	Status          string
	StartTime       string
	TestTime        string
	EngineVersion   string
	CriteriaVersion string
	Endpoints       []Endpoint
}

type Server struct {
	Address  string
	SslGrade string
	Country  string
	Owner    string
}

type Dominio struct {
	Host             string
	ServersChanged   bool
	SslGrade         string
	PreviousSslGrade string
	Logo             string
	Title            string
	IsDown           bool
	Servers          []Server
}

type WhosIpResponse struct {
	Country string
	Owner   string
}

type DomainPage struct {
	Logo  string
	Title string
}

type DomainList struct {
	items []Dominio
}
