package at_commands

import (
	"debug_print_go"
	"fmt"
	"github.com/jacobsa/go-serial/serial"
	"io"
	"os"
	"strconv"
)

const (
	BUFSIZE    = 4096
	SMBUF      = 32
	NO_COMMAND = iota
	NONE
	FIRST_R
	COMMAND_RESPONSE
	RESPONSE
	NOT_A_COMMAND
	FIN_R
	COMMA_SEP_SPACE
	COMMA_SEP
	READ_DATA
	CTRL_Z     = "\x1a"
	CHTTPACT   = "+CHTTPACT"
	REQUEST    = "REQUEST"
	ERROR      = "ERROR"
	CME        = "CME"
	DATA       = "DATA"
	OK         = "OK"
	NO         = "NO"
	CARRIER    = "CARRIER"
	NO_CARRIER = "NO CARRIER"
	BPS_115200 = "115200"
	CR         = "\r"
	LF         = "\n"
)

const DEBUG = false

type CommandResponse struct {
	Name   string
	Params []string
	Data   []byte
	Status string
}

var Commands chan []byte
var Responses chan *CommandResponse
var connected bool

func init() {
	connected = false
}

func ConnectToPort() {
	if connected {
		return
	}
	ch := make(chan []byte)
	Responses = make(chan *CommandResponse)
	Commands = make(chan []byte)

	const fake = false

	if fake {
		go fakePortReader(ch)
		go fakePortWriter(Commands)
	} else {
		port := openPort() // FIXME: we don't close the port
		go portReader(ch, port)
		go portWriter(Commands, port)
	}
	go commandParser(ch, Responses)
	connected = true
}

func openPort() io.ReadWriteCloser {
	// Set up options.
	// 115200bps, 8 bit data, no parity, 1 bit stop, no data stream control.
	options := serial.OpenOptions{
		PortName:        "/dev/ttyUSB3",
		BaudRate:        115200,
		DataBits:        8,
		StopBits:        1,
		MinimumReadSize: 4,
	}

	// Open the port.
	port, err := serial.Open(options)
	if err != nil {
		printer.Fatal(err, "serial.Open")
	}

	// Make sure to close it later.
	return port
}

// TODO: use bufio.Reader instead
func portReader(ch chan<- []byte, port io.ReadWriteCloser) {
	buf := make([]byte, BUFSIZE)
	for {
		n, err := port.Read(buf)
		if err != nil {
			if err == io.EOF {
				printer.Error(err, "No more data from port")
			} else {
				printer.Error(err, "An error occured")
			}
			close(ch)
			return
		}
		// printer.Debug(n, "Got bytes")
		sendBuf := make([]byte, n)
		copy(sendBuf, buf)
		printer.Debug(string(sendBuf), "Got data")
		ch <- sendBuf
	}
}

func portWriter(Commands chan []byte, port io.ReadWriteCloser) {
	for b := range Commands {
		for {
			n, err := port.Write(b)
			if err != nil {
				// FIXME: do a graceful exit
				printer.Fatal(err, "port.Write")
			}
			printer.Note(b[:n], fmt.Sprintf("Sent data (%d bytes)", n))
			if n == len(b) {
				break
			}
			b = b[n:]
		}
	}
}

func fakePortReader(ch chan<- []byte) {
	file, err := os.Open("req12")
	if err != nil {
		printer.Fatal(err, "Could not open log file")
	}
	defer file.Close()
	buf := make([]byte, SMBUF)
	for {
		n, err := file.Read(buf)
		if err == io.EOF {
			printer.Note("File ended")
			close(ch)
			return
		}
		// printer.Debug(n, "Got bytes")
		sendBuf := make([]byte, n)
		copy(sendBuf, buf)
		ch <- sendBuf
		// time.Sleep(time.Millisecond * 50)
	}
}

func fakePortWriter(Commands chan []byte) {
	for command := range Commands {
		printer.Debug(command, "fakePortWriter: new command")
	}
}

func strState(state int) string {
	switch state {
	case NO_COMMAND:
		return "NO_COMMAND"
	case NONE:
		return "NONE"
	case FIRST_R:
		return "FIRST_R"
	case COMMAND_RESPONSE:
		return "COMMAND_RESPONSE"
	case RESPONSE:
		return "RESPONSE"
	case NOT_A_COMMAND:
		return "NOT_A_COMMAND"
	case COMMA_SEP_SPACE:
		return "COMMA_SEP_SPACE"
	case COMMA_SEP:
		return "COMMA_SEP"
	case READ_DATA:
		return "READ_DATA"
	case FIN_R:
		return "FIN_R"
	default:
		return "UNKNOWN"
	}
}

// FIXME: single Ctrl-Z at the end of responded data, Ctrl-Z escaping
func commandParser(ch <-chan []byte, Responses chan<- *CommandResponse) {
	state := NO_COMMAND
	prevState := NONE
	var command []byte
	var status []byte
	var param []byte
	possibleStatuses := map[string]bool{
		OK:         true,
		ERROR:      true,
		BPS_115200: true,
		CARRIER:    true,
	}
	var cmdresp *CommandResponse
	dataRead := 0
	dataAvail := 0
	for buf := range ch {
		for _, c := range buf {

			// Debug output
			if DEBUG {
				if state != READ_DATA {
					sym := ""
					switch c {
					case ' ':
						sym = "'SPACE'"
					case '\r':
						sym = "'CR'"
					case '\n':
						sym = "'LF'"
					default:
						sym = fmt.Sprintf("%c", c)
					}
					printer.Note(sym, "c")
				}
				if prevState != state {
					printer.Note(strState(state), "Changed state to")
				} else {
					printer.Debug(strState(state), "State")
				}
				prevState = state
			}

			switch state {
			case NO_COMMAND:
				if c == '\r' {
					state = FIRST_R
				}
			case FIRST_R:
				if c == '\n' {
					cmdresp = &CommandResponse{}
					state = RESPONSE
				} else {
					state = NO_COMMAND
				}
			case RESPONSE:
				if c == ' ' {
					printer.Fatal("Command starts with space!")
				} else if c == '>' {
					// Ignore it
					printer.Note("Ignoring >")
					state = NO_COMMAND
				} else if c == '\r' {
					printer.Note("Empty prologue, repeating...")
					state = FIRST_R
				} else {
					command = make([]byte, 0, SMBUF)
					command = append(command, c)
					state = COMMAND_RESPONSE
				}
				// if c == '+' {
				// } else {
				//  status = command
				//  command = command[:0]
				//  state = NOT_A_COMMAND
				//  printer.Debug("NOT_A_COMMAND", "Change state")
				// }
			case NOT_A_COMMAND:
				if c != '\r' {
					status = append(status, c)
				} else {
					statusStr := string(status)
					if _, found := possibleStatuses[statusStr]; !found {
						printer.Fatal(statusStr, "No such status")
					}
					if statusStr == CARRIER && cmdresp.Name == NO {
						// NO CARRIER command
						cmdresp.Name = NO_CARRIER
					} else {
						cmdresp.Status = statusStr
					}
					state = FIN_R
				}
			case COMMAND_RESPONSE:
				if c == ' ' {
					cmdresp.Name = string(command)
					status = make([]byte, 0)
					state = NOT_A_COMMAND
				} else if c == '\r' {
					cmdresp.Name = string(command)
					state = FIN_R
				} else if c == ':' {
					cmdresp.Name = string(command)
					state = COMMA_SEP_SPACE
					param = make([]byte, 0)
				} else {
					command = append(command, c)
				}
			case COMMA_SEP_SPACE:
				if c == ' ' {
					state = COMMA_SEP
				} else {
					printer.Fatal("Comma-separated values without space character", "Parser error")
				}
			case COMMA_SEP:
				if c == '\r' {
					cmdresp.Params = append(cmdresp.Params, string(param))
					state = FIN_R
				} else if c == ',' {
					cmdresp.Params = append(cmdresp.Params, string(param))
					param = make([]byte, 0)
				} else {
					param = append(param, c)
				}
			case READ_DATA:
				// printer.Debug("READ_DATA","State")
				// printer.Note(fmt.Sprintf("Available %d bytes, already read %d bytes", dataAvail, dataRead))
				if dataAvail > 0 {
					cmdresp.Data[dataRead] = c
					dataRead++
					dataAvail--
				}
				if dataAvail == 0 {
					Responses <- cmdresp
					state = NO_COMMAND
				}
			case FIN_R:
				if c == '\n' {
					// if command is CHTTPACT: DATA, then read data
					if cmdresp.Name == CHTTPACT && len(cmdresp.Params) > 0 && cmdresp.Params[0] == DATA {

						printer.Debug("Got a command with DATA!", "Command parser")
						dataRead = 0
						var err error
						dataAvail, err = strconv.Atoi(cmdresp.Params[1])
						if err != nil {
							printer.Fatal("Could not convert number of bytes")
						}
						cmdresp.Data = make([]byte, dataAvail)
						state = READ_DATA
						printer.Note(fmt.Sprintf("Available %d bytes, already read %d bytes", dataAvail, dataRead))
					} else {
						state = NO_COMMAND
						printer.Note(cmdresp, "Received command")
						Responses <- cmdresp
					}
				} else {
					printer.Error("No \\n after FIN \\r")
					state = NO_COMMAND
				}
			}
		}
	}
	close(Responses)
}
