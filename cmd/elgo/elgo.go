package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/oleksandr/bonjour"
)

var brightness = flag.Uint("brightness", 0, "set brightness (between 1 and 100)")
var temperature = flag.Uint("temperature", 0, "set color temperature (between 2900 (reddish) and 7000 (blueish)")
var verbose = flag.Bool("v", false, "enable verbose output")

// From: https://help.elgato.com/hc/en-us/articles/4413403384845-mDNS-Service-Strings-for-Elgato-Devices
const service = "_elg._tcp"

// From: https://groups.google.com/a/google.com/g/spend-1000-discuss/c/lAFjaEU4GAA/m/ccK6t_KCBwAJ
const urlTemplate = "http://%s/elgato/lights"

func getMDNS() (hostName string, err error) {
	wg := &sync.WaitGroup{}
	r, err := bonjour.NewResolver(nil)
	if err != nil {
		return "", err
	}
	svcs := make(chan *bonjour.ServiceEntry)
	wg.Add(1)
	go func() {
		svc := <-svcs
		if *verbose {
			log.Printf("Service: %+v", svc)
		}
		hostName = fmt.Sprintf("%s:%d", svc.HostName, svc.Port)
		r.Exit <- true
		wg.Done()
	}()
	if err := r.Browse(service, "", svcs); err != nil {
		return "", err
	}
	wg.Wait()
	return hostName, nil
}

type light struct {
	On          int `json:"on"` // 1 or 0, always include it
	Brightness  int `json:"brightness,omitempty"`
	Temperature int `json:"temperature,omitempty"`
}

type state struct {
	NumberOfLights int     `json:"numberOfLights"`
	Lights         []light `json:"lights"`
}

// From: https://docs.google.com/spreadsheets/d/1QqLaonLxfAmD5vcyXd_9u8FkxbFoNYMQhOMk4lLZS5k/edit#gid=0
const kelvinFactor = 1000000

func fromKelvin(kelvin int) int {
	return int(kelvinFactor / kelvin)
}
func toKelvin(temp int) int {
	return int(kelvinFactor / temp)
}

func getState(hostName string) state {
	url := fmt.Sprintf(urlTemplate, hostName)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	respJson, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
	r := state{}
	err = json.Unmarshal(respJson, &r)
	if err != nil {
		log.Fatal(err)
	}
	return r
}

func putState(hostName string, s state) state {
	url := fmt.Sprintf(urlTemplate, hostName)
	jsonState, err := json.Marshal(s)
	if err != nil {
		log.Fatal(err)
	}
	if *verbose {
		log.Printf("request: %s", jsonState)
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonState))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	respJson, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
	if *verbose {
		log.Printf("JSON response: %s", respJson)
	}
	r := state{}
	err = json.Unmarshal(respJson, &r)
	if err != nil {
		log.Fatalf("bad JSON response: %s", respJson)
	}
	return r
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

	hostName, err := getMDNS()
	if err != nil {
		log.Fatal(err)
	}
	if hostName == "" {
		log.Fatal("empty hostname")
	}
	if *verbose {
		log.Printf("Hostname: %s", hostName)
	}

	args := flag.Args()
	if len(args) > 1 {
		log.Fatal("only one command may be specified: on, off or toggle (default)")
	}
	if len(args) == 0 {
		args = []string{"toggle"}
	}

	command := args[0]
	s := state{
		NumberOfLights: 1,
		Lights:         []light{{}},
	}
	commandLower := strings.ToLower(command)
	switch commandLower {
	case "on":
		s.Lights[0].On = 1
	case "off":
		s.Lights[0].On = 0
	case "toggle":
		s = getState(hostName)
		if s.NumberOfLights != 1 {
			log.Fatalf("expected one light, got %d", s.NumberOfLights)
		}
		if s.Lights[0].On == 0 {
			s.Lights[0].On = 1
		} else {
			s.Lights[0].On = 0
		}

		// Don't change other properties
		s.Lights[0].Brightness = 0
		s.Lights[0].Temperature = 0
	default:
		log.Fatalf("bad command: %s", command)
	}

	if *brightness > 100 {
		log.Fatal("brightness must be between 0 and 100")
	}
	s.Lights[0].Brightness = int(*brightness)
	if *temperature != 0 {
		if *temperature < 2900 || *temperature > 7000 {
			log.Fatal("temperature must be between 2900 and 7000 (in Kelvins)")
		}
		s.Lights[0].Temperature = fromKelvin(int(*temperature))
	}

	rState := putState(hostName, s)

	if *verbose {
		log.Printf("temperature: %dK", toKelvin(rState.Lights[0].Temperature))
	}
}
