package main

import (
	"database/sql"
	"fmt"
	"html"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/gomarkdown/markdown"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
	"maunium.net/go/mautrix"
)

const version = 7

var db *sql.DB
var matrixClient *mautrix.Client
var tempDir = "temp/"
var dirPrefix string
var store *FileStore

func initDB() error {
	database, era := sql.Open("sqlite3", dirPrefix+"data.db")
	if era != nil {
		panic(era)
	}
	db = database
	return era
}

//returns true if app should exit
func initCfg() bool {
	viper.SetConfigType("json")
	viper.SetConfigFile(dirPrefix + "cfg.json")
	viper.AddConfigPath(dirPrefix)
	viper.SetConfigName("cfg")

	err := viper.ReadInConfig()
	if err != nil {
		fmt.Println("config not found. creating new one")

		rr := saveVersion(version)
		if rr != nil {
			panic(rr)
		}

		viper.SetDefault("matrixServer", "matrix.org")
		viper.SetDefault("matrixuserpassword", "AverySecretPassword21!")
		viper.SetDefault("matrixuserid", "@m:matrix.org")
		viper.SetDefault("defaultmailCheckInterval", 30)
		viper.SetDefault("markdownEnabledByDefault", true)
		viper.SetDefault("htmlDefault", false)
		viper.SetDefault("allowed_servers", [1]string{"YourMatrixServerDomain.com"})
		viper.WriteConfigAs(dirPrefix + "cfg.json")
		return true
	}

	ae := viper.GetInt("defaultmailCheckInterval")
	if ae == 0 {
		viper.SetDefault("defaultmailCheckInterval", 1)
		viper.WriteConfigAs(dirPrefix + "cfg.json")
	}

	allowedHosts := viper.GetStringSlice("allowed_servers")
	if len(allowedHosts) == 0 {
		allowedHosts = make([]string, 1)
		allowedHosts[0] = "YourMatrixServerDomain.com"
		viper.SetDefault("allowed_servers", allowedHosts)
		viper.WriteConfigAs(dirPrefix + "cfg.json")
	}

	return false
}

func loginMatrix() {
	fmt.Println("Logging into", viper.GetString("matrixserver"), "as", viper.GetString("matrixuserid"))
	client, err := mautrix.NewClient(viper.GetString("matrixserver"), "", "")
	if err != nil {
		panic(err)
	}
	_, err = client.Login(&mautrix.ReqLogin{
		Type:             "m.login.password",
		Identifier:       mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: viper.GetString("matrixuserid")},
		Password:         viper.GetString("matrixuserpassword"),
		StoreCredentials: true,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("Login successful")
	store = NewFileStore(dirPrefix+"store.json", client.UserID)
	client.Store = store
	matrixClient = client
	go startMatrixSync()
}

func getHostFromMatrixID(matrixID string) (host string, err int) {
	if strings.Contains(matrixID, ":") {
		splt := strings.Split(matrixID, ":")
		if len(splt) == 2 {
			return splt[1], -1
		}
		return "", 1
	}
	return "", 0
}

func contains(a []string, x string) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}

func logOut(client *mautrix.Client, roomID string, leave bool) error {
	stopMailChecker(roomID)
	deleteRoomAndEmailByRoomID(roomID)
	if leave {
		_, err := client.LeaveRoom(id.RoomID(roomID))
		if err != nil {
			WriteLog(critical, "#65 bot can't leave room: "+err.Error())
			return err
		}
	}
	return nil
}

func startMatrixSync() {
	fmt.Println(matrixClient.UserID)

	syncer := matrixClient.Syncer.(*mautrix.DefaultSyncer)

	syncer.OnEventType(event.StateMember, func(source mautrix.EventSource, evt *event.Event) {
		client := matrixClient
		store.UpdateRoomState(evt.RoomID, evt)
		if id.UserID(*evt.StateKey) == client.UserID {
			currentMembership, _ := store.GetMembershipState(evt.RoomID)
			if source == mautrix.EventSourceInvite|mautrix.EventSourceState && currentMembership == event.MembershipInvite {
				fmt.Println("invited...")
				host, err := getHostFromMatrixID(string(evt.Sender))
				if err == -1 {
					listcontains := contains(viper.GetStringSlice("allowed_servers"), host)
					if listcontains {
						client.JoinRoomByID(evt.RoomID)
						client.SendText(evt.RoomID, "Hey you have invited me to a new room. Enter !login to bridge this room to a Mail account")
					} else {
						client.LeaveRoom(evt.RoomID)
						WriteLog(info, string("Got invalid invite from "+evt.Sender+" reason: senders server not whitelisted! Adjust your config if you want to allow this host using me"))
						return
					}
				} else {
					WriteLog(critical, "")
				}
			}
			if source == mautrix.EventSourceLeave|mautrix.EventSourceTimeline && currentMembership == event.MembershipLeave {
				fmt.Println("leaving...")
				logOut(client, string(evt.RoomID), true)
			}
		}
	})

	syncer.OnEventType(event.EventMessage, func(source mautrix.EventSource, evt *event.Event) {
		if evt.Sender == matrixClient.UserID {
			return
		}
		currentMembership, timestamp := store.GetMembershipState(evt.RoomID)
		if currentMembership == event.MembershipLeave || timestamp > evt.Timestamp {
			return
		}
		message := evt.Content.AsMessage().Body
		roomID := evt.RoomID

		if is, err := isUserWritingEmail(string(roomID)); is && err == nil {
			writingEmail(evt, message)
		} else if err != nil {
			WriteLog(critical, "#41 deleteWritingTemp: "+err.Error())
			matrixClient.SendText(roomID, "An server-error occured Errorcode: #41")
			return
		} else {
			//commands only available in room not bridged to email
			runCommand(message, evt)
		}
	})

	err := matrixClient.Sync()
	if err != nil {
		WriteLog(logError, "#07 Syncing: "+err.Error())
		fmt.Println(err)
	}
}

func viewViewHelp(roomID string) {
	matrixClient.SendText(id.RoomID(roomID), "Available options:\n\nmb/mailbox\t-\tViews the current used mailbox\nmbs/mailboxes\t-\tView the available mailboxes\nbl/blocklist\t-\tViews the list of blocked addresses")
}

func deleteTempFile(name string) {
	os.Remove(tempDir + name)
}

func streamToTempFile(stream io.ReadCloser, file string) error {
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		os.Mkdir(tempDir, os.ModePerm)
	}
	fo, err := os.Create(tempDir + file)
	if err != nil {
		return err
	}

	defer func() {
		if err := fo.Close(); err != nil {

		}
	}()

	buf := make([]byte, 1024)
	for {
		n, err := stream.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if _, err := fo.Write(buf[:n]); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	dirPrefix = "./"
	if len(os.Getenv("BRIDGE_DATA_PATH")) > 0 {
		argDir := os.Getenv("BRIDGE_DATA_PATH")
		if !strings.HasPrefix(argDir, "/") && !strings.HasPrefix(argDir, "./") {
			argDir = "./" + argDir
		}
		if !strings.HasSuffix(argDir, "/") {
			argDir = argDir + "/"
		}
		s, err := os.Stat(argDir)
		if err != nil {
			err = os.Mkdir(argDir, 0750)
			if err != nil {
				fmt.Printf("Error creating dir %s\n%s\n", argDir, err.Error())
				os.Exit(1)
				return
			}
		} else {
			if !s.IsDir() {
				fmt.Printf("%s is not a dir!\n", argDir)
				os.Exit(1)
				return
			}
		}
		dirPrefix = argDir
	}
	tempDir = dirPrefix + tempDir

	initLogger()

	er := initDB()
	if er == nil {
		createAllTables()
		exit := initCfg()
		if exit {
			return
		}
		WriteLog(success, "create tables")
		handleDBVersion()
	} else {
		WriteLog(critical, "#08 creating tables: "+er.Error())
		panic(er)
	}

	deleteAllWritingTemps()

	loginMatrix()

	startMailSchedeuler()

	for {
		time.Sleep(1 * time.Second)
	}
}

func stopMailChecker(roomID string) {
	_, ok := listenerMap[roomID]
	if ok {
		close(listenerMap[roomID])
		//delete(listenerMap, evt.RoomID)
	}
}

var listenerMap map[string]chan bool
var clients map[string]*client.Client
var imapErrors map[string]*imapError
var checksPerAccount map[string]int

const maxRoomChecks = 15

const maxErrUntilReconnect = 10

type imapError struct {
	retryCount, loginErrCount int
}

func getChecksForAccount(roomID string) int {
	checks, ok := checksPerAccount[roomID]
	if ok {
		return checks
	}
	checksPerAccount[roomID] = 0
	return 0
}

func hasError(roomID string) (has bool, count int) {
	_, ok := imapErrors[roomID]
	if ok {
		return true, imapErrors[roomID].retryCount
	}
	return false, -1
}

func startMailSchedeuler() {
	listenerMap = make(map[string]chan bool)
	clients = make(map[string]*client.Client)
	imapErrors = make(map[string]*imapError)
	checksPerAccount = make(map[string]int)

	accounts, err := getimapAccounts()
	if err != nil {
		WriteLog(critical, "#09 reading accounts: "+err.Error())
		log.Panic(err)
	}
	for i := 0; i < len(accounts); i++ {
		go startMailListener(accounts[i])
	}
	WriteLog(success, "started "+strconv.Itoa(len(accounts))+" mail listener")
}

func startMailListener(account imapAccountount) {
	quit := make(chan bool)
	connectSuccess := false
	var mClient *client.Client
	var err error
	for !connectSuccess {
		mClient, err = loginMail(account.host, account.username, account.password, account.ignoreSSL)
		if err == nil {
			connectSuccess = true
			continue
		} else {
			WriteLog(info, "couldn't connect to imap server try again n a some minutes: "+err.Error())
			time.Sleep(1 * time.Minute)
		}
	}

	listenerMap[account.roomID] = quit
	clients[account.roomID] = mClient
	go func() {
		for {
			select {
			case <-quit:
				return
			default:
				if getChecksForAccount(account.roomID) >= maxRoomChecks {
					reconnect(account)
					return
				}
				fetchNewMails(mClient, &account)
				checksPerAccount[account.roomID]++
				time.Sleep((time.Duration)(account.mailCheckInterval) * time.Second)
			}
		}
	}()
}

func reconnect(account imapAccountount) {
	WriteLog(info, "reconnecting account "+account.username)
	checksPerAccount[account.roomID] = 0
	stopMailChecker(account.roomID)
	nacc := account
	go startMailListener(nacc)
}

func fetchNewMails(mClient *client.Client, account *imapAccountount) {
	messages := make(chan *imap.Message, 1)
	section, errCode := getMails(mClient, account.mailbox, messages)

	if section == nil {
		if errCode == 0 {
			haserr, errCount := hasError(account.roomID)
			if haserr {
				if imapErrors[account.roomID].loginErrCount > 15 {
					WriteLog(logError, "Youve got too much errors for the emailaccount: "+account.username)
				}
				if errCount < maxErrUntilReconnect {
					imapErrors[account.roomID].retryCount++
				} else {
					imapErrors[account.roomID].retryCount = 0
					imapErrors[account.roomID].loginErrCount++
					reconnect(*account)
					return
				}
			}
		}
		if account.silence {
			account.silence = false
		}
		return
	}

	for msg := range messages {
		mailID := msg.Envelope.Subject + strconv.Itoa(int(msg.InternalDate.Unix()))
		if has, err := dbContainsMail(mailID, account.roomPKID); !has && err == nil {
			go insertEmail(mailID, account.roomPKID)
			if !account.silence {
				handleMail(msg, section, *account)
			}
		} else if err != nil {
			WriteLog(logError, "#11 dbContains mail: "+err.Error())
			fmt.Println(err.Error())
		}
	}
	if account.silence {
		account.silence = false
	}
}

func handleMail(mail *imap.Message, section *imap.BodySectionName, account imapAccountount) {
	content := getMailContent(mail, section, account.roomID)
	if content == nil {
		return
	}
	for _, senderMail := range content.sendermails {
		fmt.Println("checking", senderMail)
		if checkForBlocklist(account.roomID, senderMail) {
			fmt.Println("blocked email from ", senderMail)
			return
		}
	}
	from := html.EscapeString(content.from)
	fmt.Println("attachments: " + content.attachment)
	plainContent := "You've got a new Email from " + from + "\r\n" + "Subject: " + content.subject + "\r\n" + content.body
	if content.htmlFormat {
		body = string(markdown.ToHTML([]byte(content.body), nil, nil))
		htmlContent := &event.MessageEventContent{
			Format:        event.FormatHTML,
			Body:          plainContent,
			FormattedBody: "<b>You've got a new Email</b> from <b>" + from + "</b><br>" + "Subject: " + content.subject + "<br>────────────────<br>" + body,
			MsgType:       event.MsgText,
		}
		matrixClient.SendMessageEvent(id.RoomID(account.roomID), event.EventMessage, &htmlContent)
	} else {
		matrixClient.SendText(id.RoomID(account.roomID), plainContent)
	}
}
