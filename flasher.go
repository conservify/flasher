package main

import (
	"flag"
	"fmt"
	"go.bug.st/serial.v1"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type configuration struct {
	Board          string
	Port           string
	Binary         string
	Tools          string
	SkipTouch      bool
	Tail           bool
	TailAppend     string
	TailInactivity int
	TailReopen     bool
}

func searchForTools(config *configuration) string {
	if config.Tools != "" {
		return config.Tools
	}

	exec, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	dir, err := filepath.Abs(filepath.Dir(exec))
	if err != nil {
		log.Fatal(err)
	}

	candidates := []string{
		filepath.Join(dir, "tools"),
		filepath.Join(filepath.Dir(dir), "lib/flasher"),
		"./tools",
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			return p
		}
	}

	panic(fmt.Sprintf("Unable to find tools, looked in %v", candidates))
}

func openSerial(config *configuration) (serial.Port, error) {
	mode := &serial.Mode{
		BaudRate: 115200,
	}
	port, err := serial.Open(config.Port, mode)
	if err != nil {
		log.Fatalf("Unable to open %s: %v", config.Port, err)
	}

	return port, nil
}

func echoSerial(config *configuration, port serial.Port, c *chan bool) {
	defer port.Close()

	var file *os.File
	if config.TailAppend != "" {
		log.Printf("Logging to %s", config.TailAppend)
		file, err := os.OpenFile(config.TailAppend, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatalf("Unable to open %s: %v", config.TailAppend, err)
		}
		defer file.Close()
	}

	buff := make([]byte, 256)

	for {
		n, err := port.Read(buff)
		if err != nil {
			log.Printf("Error reading: %v", err)
			break
		}
		if n == 0 {
			break
		}
		*c <- true
		// This is probably controversial:
		sanitized := strings.Replace(string(buff[:n]), "\r", "", -1)
		fmt.Printf("%v", sanitized)
		file.WriteString(sanitized)
	}

	log.Printf("Tailing done.")
}

func main() {
	config := configuration{}

	flag.StringVar(&config.Board, "board", "adafruit_feather_m0", "board to upload to")
	flag.StringVar(&config.Port, "port", "", "port to upload to")
	flag.StringVar(&config.Binary, "binary", "", "path to the binary (required)")
	flag.StringVar(&config.Tools, "tools", "", "path to the tools directory")
	flag.BoolVar(&config.SkipTouch, "skip-touch", false, "skip the touch")
	flag.BoolVar(&config.Tail, "tail", false, "show serial")
	flag.StringVar(&config.TailAppend, "append", "", "append tail to file")
	flag.IntVar(&config.TailInactivity, "tail-inactivity", 0, "inactive time until quitting tail")
	flag.BoolVar(&config.TailReopen, "tail-reopen", false, "tail again after inactivity or file loss")
	flag.Parse()

	if config.Binary == "" && !config.Tail {
		flag.Usage()
		os.Exit(2)
	}

	pd := NewPortDiscoveror()

	if config.Binary != "" {
		if _, err := os.Stat(config.Binary); os.IsNotExist(err) {
			log.Fatalf("Error: No such binary '%s'", config.Binary)
		}

		config.Tools = searchForTools(&config)

		boardsPath := path.Join(config.Tools, "boards.txt")
		boards, err := NewPropertiesMapFromFile(boardsPath)
		if err != nil {
			log.Fatalf("Error: Unable to open %s (%v)", boardsPath, err)
		}

		platformPath := path.Join(config.Tools, "platform.txt")
		platform, err := NewPropertiesMapFromFile(platformPath)
		if err != nil {
			log.Fatalf("Error: Unable to open %s (%v)", platformPath, err)
		}

		portPath, err := filepath.EvalSymlinks(config.Port)
		if err != nil {
			log.Fatalf("Unable to evaluate symlinks %s (%v)", config.Port, err)
		}

		Upload(&UploadOptions{
			Boards:    boards,
			Platform:  platform,
			SkipTouch: config.SkipTouch,
			Board:     config.Board,
			Port:      portPath,
			Binary:    config.Binary,
			Tools:     config.Tools,
		})
	}

	if config.Tail {
		time.Sleep(1 * time.Second)

		for {
			if _, err := os.Stat(config.Port); os.IsNotExist(err) {
				log.Printf("Port '%s' disappeared, scanning...", config.Port)
				config.Port = pd.discover()
				if config.Port == "" {
					log.Fatalf("Error: Unable to find port to tail.")
				}
			}

			ch := make(chan bool)

			port, err := openSerial(&config)
			if err != nil {
				log.Fatalf("Error: Unable to open port: %v", err)
			}

			go echoSerial(&config, port, &ch)

			go func() {
				for {
					time.Sleep(1 * time.Second)
					ch <- false
				}
			}()

			previous := time.Now()

			for {
				data := <-ch
				if data {
					previous = time.Now()
				}

				if config.TailInactivity > 0 {
					ago := time.Duration(-config.TailInactivity) * time.Second
					to := time.Now().Add(ago)
					if previous.Before(to) {
						port.Close()
						log.Printf("Tail inactive!")
						break
					}
				}
			}

			if !config.TailReopen {
				break
			}
		}
	}
}
