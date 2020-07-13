package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"text/template"
	"time"

	"github.com/urfave/cli"
)

func main() {

	//Declaring file names
	var templateFileName, recipientListFileName, configFileName string

	//For reading the template and recipientList filepaths, cli is utilized
	// https://github.com/urfave/cli
	app := &cli.App{
		Name:  "bmail",
		Usage: "Send Bulk Emails",
		//cli flags take Filepath from user
		Flags: []cli.Flag{
			//cli flag for taking TemplateFilepath from user
			&cli.StringFlag{
				Name:     "template, t",
				Usage:    "Load HTML template from `FILE`",
				Required: true,
			},

			//cli flag for taking RecipientlistFilepath from user
			&cli.StringFlag{
				Name:     "recipient, r",
				Usage:    "Load recipient list (csv) from `FILE`",
				Required: true,
			},
			//cli flag for taking SMTPConfigFilepath from user
			&cli.StringFlag{
				Name:     "config, c",
				Usage:    "Load SMTPConfig File (csv) from `FILE`",
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {

			templateFileName = c.String("template")
			recipientListFileName = c.String("recipient")
			configFileName = c.String("config")
			fmt.Println("Use bmail --template TEMPLATEFILE.html --recipient RECIPIENTLIST.csv")
			return nil
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}

	//from := "mail@example.com"
	subject := "Test Mail"

	serverCount, username, password, hostname, port := ParseServerConfig(configFileName)

	runtime.GOMAXPROCS(0) //Golang sets it to number of cores by default

	var wg sync.WaitGroup
	wg.Add(serverCount)
	recordLength := VerifyCSV(recipientListFileName, configFileName)
	SplitRecipients(recipientListFileName, serverCount, recordLength)

	for i := 0; i < serverCount; i++ {
		serverstruct := ServerConfig{
			username: username[i],
			password: password[i],
			hostname: hostname[i],
			port:     port[i],
		}
		j := strconv.Itoa(i + 1)
		filename := "BMail_recipientList" + j + ".csv"
		go ReadRecipient(filename, templateFileName, configFileName, subject, username[i], serverstruct, &wg)
	}
	//wait for all exectutions to finish
	fmt.Println("Waiting To Finish")
	wg.Wait()
	fmt.Println("\nTerminating Program and removing temporary files")
	for k := 0; k < serverCount; k++ {
		j := strconv.Itoa(k + 1)
		filename := "BMail_recipientList" + j + ".csv"
		os.Remove(filename)
	}

}

//ServerConfig structure
type ServerConfig struct {
	username string
	password string
	hostname string
	port     string
}

//Message structure
type Message struct {
	to      string
	from    string
	subject string
	body    string
}

//SplitRecipients splits recipient files
//into different files according to the number of
//smtp server relays available
func SplitRecipients(recipientListFileName string, serverCount, recordLength int) {

	var array []int
	array = append(array, 0)
	array = append(array, (recordLength / serverCount))
	for i := 1; i <= serverCount; i++ {
		array = append(array, (recordLength / serverCount))
	}
	for i := 1; i <= (recordLength % serverCount); i++ {
		array[i] = array[i] + 1
	}

	//Opens the recipientListFile
	recipientListFile, err := os.Open(recipientListFileName)
	if err != nil {
		fmt.Println(err)
	}
	defer recipientListFile.Close()
	reader := csv.NewReader(recipientListFile)
	for i := 1; i <= serverCount; i++ {
		j := strconv.Itoa(i)
		filename := "BMail_recipientList" + j + ".csv"
		recipientListFile, err := os.Create(filename)
		if err != nil {
			fmt.Println(err)
		}
		writer := csv.NewWriter(recipientListFile)
		for k := 0; k < array[i]; k++ {
			record, err := reader.Read()
			if err != nil {
				if err == io.EOF {
					break
				}
				fmt.Println(err)
				return
			}
			err = writer.Write(record)
			if err != nil {
				fmt.Println(err)
				return
			}

		}
		writer.Flush()
		err = recipientListFile.Close()
		if err != nil {
			fmt.Println(err)
			return
		}
	}
}

//VerifyCSV verifies all the csv files
//error is thrown if it fails
//returns number of records in recipientList
func VerifyCSV(recipientListFileName, configFileName string) int {

	//for counting number of records in recipientList
	var recordNo int = 0
	//Validates recipientListFile
	recipientListFile, err := os.Open(recipientListFileName)
	if err != nil {
		log.Fatalln("Couldn't open the recipientlist file : ", recipientListFileName, err)
	}
	// Parse the file
	readRecipient := csv.NewReader(bufio.NewReader(recipientListFile))
	// Iterate through the records
	// Read each record from csv
	for {
		_, err = readRecipient.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal("Error while parsing recipientlist file : ", recipientListFileName, err)
		}
		recordNo++
	}

	//Validates configFile
	configFile, err := os.Open(configFileName)
	if err != nil {
		log.Fatalln("Couldn't open the recipientlist file : ", configFileName, err)
	}
	// Parse the file
	readConfig := csv.NewReader(bufio.NewReader(configFile))
	// Read each record from csv
	_, err = readConfig.ReadAll()
	if err != nil {
		log.Fatal("Error while parsing recipientlist file : ", configFileName, err)
	}
	return recordNo

}

//ParseTemplate parses the HTML template
//with data inserted into {{.}} fields
func ParseTemplate(templateFileName string, data interface{}) string {

	// Open the file
	tmpl, err := template.ParseFiles(templateFileName)
	if err != nil {
		log.Fatalln("Couldn't open the template", err)
	}
	buf := new(bytes.Buffer)
	//data is inserted into {{.}} fields
	tmpl.Execute(buf, data)
	return buf.String()
}

//ParseServerConfig parsess the config file
func ParseServerConfig(configFileName string) (int, []string, []string, []string, []string) {
	//
	// Reading config file
	//
	configFile, errc := os.Open(configFileName)
	if errc != nil {
		log.Fatalln("Couldn't open the csv file", errc)
	}
	// Parse the file
	r := csv.NewReader(bufio.NewReader(configFile))

	var username, password, hostname, port []string
	var count int = 0
	for {
		// Read each smtp records details from csv
		serverRecord, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		username = append(username, serverRecord[0])
		password = append(password, serverRecord[1])
		hostname = append(hostname, serverRecord[2])
		port = append(port, serverRecord[3])
		count++
	}
	return count, username, password, hostname, port
}

//ReadRecipient parses list of recipients from csv file
//It also calls the SendEmail function
func ReadRecipient(recipientListFileName, templateFileName, configFileName, subject, from string, serverstruct ServerConfig, wg *sync.WaitGroup) {

	defer wg.Done()
	//
	// Reading recipient list file
	//
	// Open the recipient list
	csvFile, err := os.Open(recipientListFileName)
	if err != nil {
		log.Fatalln("Couldn't open the csv file", err)
	}
	// Parse the file
	reader := csv.NewReader(bufio.NewReader(csvFile))

	// Iterate through the records
	for {
		// Read each record from csv
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		//validating email structure using regex
		erre := ValidateFormat(record[1])
		if erre != nil {
			fmt.Println("Email address (", record[1], ") is not valid - Skipping...")
		}
		if erre == nil {
			//Structure for sending data to
			data := struct {
				Name string
			}{
				Name: record[0],
			}
			//Parsing data to template (i.e "Name" in place of {{.Name}})
			body := ParseTemplate(templateFileName, data)
			m := Message{
				to:      record[1],
				subject: subject,
				body:    body,
				from:    from,
			}
			m.SendEmail(serverstruct.username, serverstruct.password, serverstruct.hostname, serverstruct.port)

		}
	}
}

//SendEmail sends the email with ServerInfo and Message details
func (m *Message) SendEmail(username, password, hostname, port string) {
	// Set up authentication information.
	//i, _ := strconv.Atoi(port)
	auth := smtp.PlainAuth("", username, password, hostname)
	addr := hostname + ":" + port

	//Convert "to" to []string
	to := []string{m.to}
	//RFC 822-style email format
	//Omit "to" parameter in msg to send as bcc
	msg := []byte("From: " + m.from + "\r\n" +
		"To: " + m.to + "\r\n" +
		"Subject: " + m.subject + "\r\n" +
		"MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n" +
		"\r\n" +
		m.body + "\r\n")

	err := smtp.SendMail(addr, auth, username, to, msg)
	//If an error occurs while sending emails, it will try 10 times(waiting for 2 seconds each time)
	count := 0
	for err != nil && count <= 10 {
		time.Sleep(2 * time.Second)
		err = smtp.SendMail(addr, auth, username, to, msg)
		count++
	}
	if err != nil {
		fmt.Println(err)
		fmt.Println("Failed sending to ", m.to)
	} else {
		fmt.Println("Email Sent to ", m.to)
	}

}

// ValidateFormat validates the format of email
// Uses regular expression for validation
func ValidateFormat(email string) error {
	regex := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
	if !regex.MatchString(email) {
		return errors.New("Invalid Format")
	}
	return nil
}
