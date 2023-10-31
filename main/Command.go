package main

import (
	"crypto/tls"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gomarkdown/markdown"
	"github.com/spf13/viper"
	"gopkg.in/gomail.v2"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type CommandHandler func(evt *event.Event, message string)

var commands = map[string]CommandHandler{
	"!help":       help,
	"!login":      login,
	"!logout":     logout,
	"!setup":      setup,
	"!write":      write,
	"!ping":       ping,
	"!setmailbox": setMailbox,
	"!sethtml":    setHtml,
	"!leave":      leave,
	"!blocklist":  blocklist,
	"!bl":         blocklist,
	"!view":       view,
	"!check":      check,
}

func check(evt *event.Event, message string) {
	checkNewEmail(evt.RoomID.String(), matrixClient)
}

func help(evt *event.Event, message string) {
	helpText := "-------- Help --------\r\n"
	helpText += "!setup imap/smtp, host:port, username(em@ail.com), password, <mailbox (only for imap)>, ignoreSSLcert(true/false) - creates a bridge for this room\r\n"
	helpText += "!ping - gets information about the email bridge for this room\r\n"
	helpText += "!help - shows this command help overview\r\n"
	helpText += "!write (receiver(s) email(s) splitted by space!) <markdown default:true> - sends an email to a given address\r\n"
	helpText += "!view - run it to see available optinos\r\n"
	helpText += "!setmailbox (mailbox) - changes the mailbox for the room\r\n"
	helpText += "!sethtml (on/off or true/false) - sets HTML-rendering for messages on/off\r\n"
	helpText += "!logout - remove email bridge from current room\r\n"
	helpText += "!leave - unbridge the current room and kick the bot\r\n"
	helpText += "\r\n---- Email writing commands ----\r\n"
	helpText += "!send - sends the email\r\n"
	helpText += "!rm <file> - removes given attachment from email\r\n"
	matrixClient.SendText(evt.RoomID, helpText)
}

func login(evt *event.Event, message string) {
	matrixClient.SendText(evt.RoomID, "Okay send me the data of your server(at first IMAPs) in the given order, splitted by a comma(,)\r\n!setup imap, host:port, username/email, password, mailbox, ignoreSSL\r\n!setup smtp, host, port, email, password, ignoreSSL\r\n\r\nExample: \r\n!setup imap, host.com:993, mail@host.com, w0rdp4ss, INBOX, false\r\nor\r\n!setup smtp, host.com:587, mail@host.com, w0rdp4ss, false")
}

func logout(evt *event.Event, mesage string) {
	roomID := evt.RoomID
	err := logOut(matrixClient, roomID.String(), false)
	if err != nil {
		matrixClient.SendText(roomID, "Error logging out: "+err.Error())
	} else {
		matrixClient.SendText(roomID, "Successfully logged out")
	}
}

func setup(evt *event.Event, message string) {
	roomID := evt.RoomID
	data := strings.Trim(strings.ReplaceAll(message, "!setup", ""), " ")
	s := strings.Split(data, ",")
	if len(s) < 4 || len(s) > 6 {
		matrixClient.SendText(roomID, "Wrong syntax :/\r\nExample: \r\n!setup imap, host.com:993, mail@host.com, w0rdp4ss, INBOX, false\r\nor\r\n"+
			"!setup smtp, host.com:587, mail@host.com, w0rdp4ss, false")
	} else {
		accountType := s[0]
		if strings.ToLower(accountType) != "imap" && strings.ToLower(accountType) != "smtp" {
			matrixClient.SendText(roomID, "What? you can setup 'imap' and 'smtp', not \""+accountType+"\"")
			return
		}
		host := strings.ReplaceAll(s[1], " ", "")
		username := strings.ReplaceAll(s[2], " ", "")
		password := strings.ReplaceAll(s[3], " ", "")
		ignoreSSlCert := false
		mailbox := "INBOX"
		if len(s) >= 5 {
			mailbox = strings.ReplaceAll(s[4], " ", "")
		}
		var err error
		defaultMailSyncInterval := viper.GetInt("defaultmailCheckInterval")
		imapAccID, smtpAccID, erro := getRoomAccounts(string(roomID))
		if erro != nil {
			matrixClient.SendText(roomID, "Something went wrong! Contact the admin. Errorcode: #37")
			WriteLog(critical, "#37 checking getRoomAccounts: "+erro.Error())
			return
		}
		if accountType == "imap" {
			if len(s) == 6 {
				ignoreSSlCert, err = strconv.ParseBool(strings.ReplaceAll(s[5], " ", ""))
				if err != nil {
					fmt.Println(err.Error())
					ignoreSSlCert = false
				}
			}
			if imapAccID != -1 {
				matrixClient.SendText(roomID, "IMAP account already existing. Create a new room if you want to use a different account!")
				return
			}
			isInUse, err := isImapAccountAlreadyInUse(username)
			if err != nil {
				matrixClient.SendText(roomID, "Something went wrong! Contact the admin. Errorcode: #03")
				WriteLog(critical, "#03 checking isImapAccountAlreadyInUse: "+err.Error())
				return
			}

			if isInUse {
				matrixClient.SendText(roomID, "This email is already in Use! You cannot use your email twice!")
				return
			}

			go func() {
				if !strings.Contains(host, ":") {
					host += ":993"
				}

				mclient, err := loginMail(host, username, password, ignoreSSlCert)
				if mclient != nil && err == nil {
					has, er := hasRoom(string(roomID))
					if er != nil {
						matrixClient.SendText(roomID, "An error occured! contact your admin! Errorcode: #25")
						WriteLog(critical, "checking imapAcc #25: "+er.Error())
						return
					}
					var newRoomID int64
					if !has {
						newRoomID = insertNewRoom(string(roomID), defaultMailSyncInterval)
						if newRoomID == -1 {
							matrixClient.SendText(roomID, "An error occured! contact your admin! Errorcode: #26")
							WriteLog(critical, "checking insertNewRoom #26")
							return
						}
					} else {
						id, err := getRoomPKID(evt.RoomID.String())
						if err != nil {
							WriteLog(critical, "checking getRoomPKID #27: "+err.Error())
							matrixClient.SendText(roomID, "An error occured! contact your admin! Errorcode: #27")
							return
						}
						newRoomID = int64(id)
					}
					imapID, succes := insertimapAccountount(host, username, password, mailbox, ignoreSSlCert)
					if !succes {
						matrixClient.SendText(roomID, "sth went wrong. Contact your admin")
						return
					}
					err = saveImapAcc(string(roomID), int(imapID))
					if err != nil {
						WriteLog(critical, "saveImapAcc #35 : "+err.Error())
						matrixClient.SendText(roomID, "sth went wrong. Contact you admin! Errorcode: #35")
						return
					}
					matrixClient.SendText(roomID, "Bridge created successfully!\r\nYou should delete the message containing your credentials ;)\r\nIMAP:\r\n"+
						"host: "+host+"\r\n"+
						"username: "+username+"\r\n"+
						"mailbox: "+mailbox+"\r\n"+
						"ignoreSSL: "+strconv.FormatBool(ignoreSSlCert))

					startMailListener(imapAccountount{host, username, password, roomID.String(), mailbox, ignoreSSlCert, int(newRoomID), defaultMailSyncInterval, true})
					WriteLog(success, "Created new bridge and started maillistener\r\n")
				} else {
					matrixClient.SendText(roomID, "Error creating bridge! Errorcode: #04\r\nReason: "+err.Error())
					WriteLog(logError, "#04 creating bridge: "+err.Error())
				}
			}()
		} else if accountType == "smtp" {
			if smtpAccID != -1 {
				matrixClient.SendText(roomID, "SMTP account already existing. Create a new room if you want to use a different account!")
				return
			}
			isInUse, err := isSMTPAccountAlreadyInUse(username)
			if err != nil {
				matrixClient.SendText(roomID, "Something went wrong! Contact the admin. Errorcode: #24")
				WriteLog(critical, "#24 checking isSMTPAccountAlreadyInUse: "+err.Error())
				return
			}
			if isInUse {
				matrixClient.SendText(roomID, "This smtp-username is already in Use! You cannot use your email twice!")
				return
			}

			go func() {
				if len(s) == 5 {
					ignoreSSlCert, err = strconv.ParseBool(strings.ReplaceAll(s[4], " ", ""))
					if err != nil {
						fmt.Println(err.Error())
						ignoreSSlCert = false
					}
				}
				has, er := hasRoom(roomID.String())
				if er != nil {
					matrixClient.SendText(roomID, "An error occured! contact your admin! Errorcode: #28")
					WriteLog(critical, "checking imapAcc #28: "+er.Error())
					return
				}
				var newRoomID int64
				if !has {
					newRoomID = insertNewRoom(roomID.String(), defaultMailSyncInterval)
					if newRoomID == -1 {
						matrixClient.SendText(roomID, "An error occured! contact your admin! Errorcode: #29")
						WriteLog(critical, "checking insertNewRoom #29: ")
						return
					}
				} else {
					id, err := getRoomPKID(evt.RoomID.String())
					if err != nil {
						WriteLog(critical, "checking getRoomPKID #30: "+err.Error())
						matrixClient.SendText(roomID, "An error occured! contact your admin! Errorcode: #30")
						return
					}
					newRoomID = int64(id)
				}
				port := 587
				if !strings.Contains(host, ":") {
					matrixClient.SendText(roomID, "No port specified! Using 587")
				} else {
					hostsplit := strings.Split(host, ":")
					host = hostsplit[0]
					port, err = strconv.Atoi(strings.Trim(hostsplit[1], " "))
					if err != nil {
						matrixClient.SendText(roomID, "The port must be a number!")
						return
					}
				}
				smtpID, err := insertSMTPAccountount(host, port, username, password, ignoreSSlCert)
				if err != nil {
					matrixClient.SendText(roomID, "sth went wrong. Contact your admin")
					return
				}
				err = saveSMTPAcc(roomID.String(), int(smtpID))
				if err != nil {
					WriteLog(critical, "saveSMTPAcc #36 : "+err.Error())
					matrixClient.SendText(roomID, "sth went wrong. Contact you admin! Errorcode: #34")
					return
				}

				matrixClient.SendText(roomID, "SMTP data saved.\r\nSMTP:\r\n"+
					"host: "+host+"\r\n"+
					"port: "+strconv.Itoa(port)+"\r\n"+
					"username: "+username+"\r\n"+
					"ignoreSSL: "+strconv.FormatBool(ignoreSSlCert))
			}()
		} else {
			matrixClient.SendText(roomID, "Not implemented yet!")
		}
	}
}

func write(evt *event.Event, message string) {
	roomID := evt.RoomID
	if has, err := hasRoom(roomID.String()); has && err == nil {
		_, smtpAccID, erro := getRoomAccounts(roomID.String())
		if erro != nil {
			WriteLog(critical, "#38 getRoomAccounts: "+erro.Error())
			matrixClient.SendText(roomID, "An server-error occured Errorcode: #38")
			return
		}
		if smtpAccID == -1 {
			matrixClient.SendText(roomID, "You have to setup an smtp account. Type !help or !login for more information")
			return
		}
		s := strings.Split(message, " ")
		if len(s) > 1 {
			receiver := strings.Trim(s[1], " ")
			if len(s) > 2 {
				receiverString := ""
				for i := 1; i < len(s); i++ {
					recEmail := strings.Trim(s[i], " ")
					if len(recEmail) == 0 || !strings.Contains(recEmail, "@") || !strings.Contains(recEmail, ".") || strings.Contains(receiverString, recEmail) {
						continue
					}
					add := ","
					if strings.HasSuffix(recEmail, ",") {
						add = ""
					}
					receiverString += strings.Trim(s[i], " ") + add
				}
				receiver = receiverString[:len(receiverString)-1]
			}

			if strings.Contains(receiver, "@") && strings.Contains(receiver, ".") && len(receiver) > 5 {
				hasTemp, err := isUserWritingEmail(roomID.String())
				if err != nil {
					WriteLog(critical, "#39 isUserWritingEmail: "+err.Error())
					matrixClient.SendText(roomID, "An server-error occured Errorcode: #39")
					return
				}
				if hasTemp {
					er := deleteWritingTemp(roomID.String())
					if er != nil {
						WriteLog(critical, "#40 deleteWritingTemp: "+er.Error())
						matrixClient.SendText(roomID, "An server-error occured Errorcode: #40")
						return
					}
				}

				mrkdwn := 0
				if viper.GetBool("markdownEnabledByDefault") {
					mrkdwn = 1
				}
				if len(s) == 3 {
					mdwn, berr := strconv.ParseBool(s[2])
					if berr == nil {
						if mdwn {
							mrkdwn = 1
						} else {
							mrkdwn = 0
						}
					}
				}

				err = newWritingTemp(roomID.String(), receiver)
				saveWritingtemp(roomID.String(), "markdown", strconv.Itoa(mrkdwn))
				if err != nil {
					WriteLog(critical, "#42 newWritingTemp: "+err.Error())
					matrixClient.SendText(roomID, "An server-error occured Errorcode: #42")
					return
				}
				matrixClient.SendText(roomID, "Now send me the subject of your email")
			} else {
				matrixClient.SendText(roomID, "this is an email: max@google.de\r\nthis is no email: "+receiver)
			}
		} else {
			matrixClient.SendText(roomID, "Usage: !write <emailaddress>")
		}
	} else {
		matrixClient.SendText(roomID, "You have to login to use this command!")
	}
}

func ping(evt *event.Event, message string) {
	roomID := evt.RoomID
	if has, err := hasRoom(roomID.String()); has && err == nil {
		roomData, err := getRoomInfo(roomID.String())
		if err != nil {
			WriteLog(logError, "#006 getRoomInfo: "+err.Error())
			matrixClient.SendText(roomID, "An server-error occured")
			return
		}

		matrixClient.SendText(roomID, roomData)
	} else {
		if err != nil {
			WriteLog(logError, "#06 hasRoom: "+err.Error())
			matrixClient.SendText(roomID, "An server-error occured")
		} else {
			matrixClient.SendText(roomID, "You have to login to use this command!")
		}
	}
}

func setMailbox(evt *event.Event, message string) {
	roomID := evt.RoomID
	imapAccID, _, erro := getRoomAccounts(roomID.String())
	if erro != nil {
		WriteLog(critical, "#48 getRoomAccounts: "+erro.Error())
		matrixClient.SendText(roomID, "An server-error occured Errorcode: #48")
		return
	}
	if imapAccID != -1 {
		d := strings.Split(message, " ")
		if len(d) == 2 {
			mailbox := d[1]
			saveMailbox(roomID.String(), mailbox)
			deleteMails(roomID.String())
			stopMailChecker(roomID.String())
			imapAccount, err := getIMAPAccount(roomID.String())
			if err != nil {
				WriteLog(critical, "#49 getIMAPAccount: "+err.Error())
				matrixClient.SendText(roomID, "An server-error occured Errorcode: #49")
				return
			}
			imapAccount.silence = true
			go startMailListener(*imapAccount)
			matrixClient.SendText(roomID, "Mailbox updated")
		} else {
			matrixClient.SendText(roomID, "Usage: !setmailbox <new mailbox>")
		}
	} else {
		matrixClient.SendText(roomID, "You have to setup an IMAP account to use this command. Use !setup or !login for more informations")
	}
}

func setHtml(evt *event.Event, message string) {
	roomID := evt.RoomID
	imapAccID, _, erro := getRoomAccounts(roomID.String())
	if erro != nil {
		WriteLog(critical, "#50 getRoomAccounts: "+erro.Error())
		matrixClient.SendText(roomID, "An server-error occured Errorcode: #50")
		return
	}
	if imapAccID != -1 {
		d := strings.Split(message, " ")
		if len(d) == 2 {
			newMode := strings.ToLower(d[1])
			newModeB := false
			if newMode == "true" || newMode == "on" {
				newModeB = true
			} else if newMode != "false" && newMode != "off" {
				matrixClient.SendText(roomID, "What?\r\non/off or true/false")
				return
			}
			err := setHTMLenabled(roomID.String(), newModeB)
			if err != nil {
				WriteLog(critical, "#56 getMailbox: "+err.Error())
				matrixClient.SendText(roomID, "An server-error occured Errorcode: #56")
				return
			}
			matrixClient.SendText(roomID, "Successfully set HTML-rendering to "+newMode)
		} else {
			matrixClient.SendText(roomID, "Usage: !sethtml (on/of) or (true/false)")
		}
	} else {
		matrixClient.SendText(roomID, "You have to setup an IMAP account to use this command. Use !setup or !login for more informations")
	}
}

func leave(evt *event.Event, message string) {
	roomID := evt.RoomID
	err := logOut(matrixClient, roomID.String(), true)
	if err != nil {
		matrixClient.SendText(roomID, "Error leaving: "+err.Error())
	} else {
		matrixClient.SendText(roomID, "Successfully unbridged")
	}
}

func blocklist(evt *event.Event, message string) {
	roomID := evt.RoomID
	imapAccID, _, _ := getRoomAccounts(roomID.String())
	if imapAccID == -1 {
		matrixClient.SendText(roomID, "You need to login with an imap account to use this command!")
		return
	}
	sm := strings.Split(message, " ")
	if len(sm) < 3 {
		if len(sm) == 2 && (sm[1] == "view" || sm[1] == "list") {
			viewBlocklist(roomID.String(), matrixClient)
		} else if len(sm) == 2 && sm[1] == "clear" {
			err := clearBlocklist(imapAccID)
			var msg string
			if err != nil {
				fmt.Println("Err:", err.Error())
				msg = "Error clearing blocklist! View logs for more details!"
			} else {
				msg = "Blocklist is now clean!"
			}
			matrixClient.SendText(roomID, msg)
		} else {
			matrixClient.SendText(roomID, "Usage: !blocklist <add/delete/clear/view> <email address>\nDon't show any emails from a given email address.\nWildcards (like *@evilEmailAddress.com) are supported")
		}
	} else {
		cmd := strings.ToLower(sm[1])
		addr := sm[2]
		if !strings.Contains(addr, "@") || !strings.Contains(addr, ".") || len(addr) < 6 {
			matrixClient.SendText(roomID, "Error! "+addr+" is an invalid email address!")
		} else {
			switch cmd {
			case "add":
				{
					//add item to blocklis
					err := addEmailToBlocklist(imapAccID, addr)
					var msg string
					if err != nil {
						fmt.Println("Err:", err.Error())
						msg = "Error adding " + addr + " to blocklist! View logs for more details!"
					} else {
						msg = "Success adding " + addr + " to blocklist!"
					}
					matrixClient.SendText(roomID, msg)
				}
			case "remove", "delete", "rm":
				{
					err := removeEmailFromBlocklist(imapAccID, addr)
					var msg string
					if err != nil {
						fmt.Println("Err:", err.Error())
						msg = "Error deleting " + addr + " from blocklist! View logs for more details!"
					} else {
						msg = "Success deleting " + addr + " from blocklist!"
					}
					matrixClient.SendText(roomID, msg)
				}
			}
		}
	}
}

func view(evt *event.Event, message string) {
	roomID := evt.RoomID
	imapAccID, _, _ := getRoomAccounts(roomID.String())
	if imapAccID == -1 {
		matrixClient.SendText(roomID, "You need to login with an imap account to use this command!")
		return
	}
	if len(message) == 0 {
		viewViewHelp(roomID.String())
	} else {
		switch strings.ToLower(message) {
		case "mb", "mailbox":
			{
				viewMailbox(roomID.String(), matrixClient)
			}
		case "mbs", "mailboxes":
			{
				viewMailboxes(roomID.String(), matrixClient)
			}
		case "blocklist", "bl", "blocklists", "blo", "blocked":
			{
				viewBlocklist(roomID.String(), matrixClient)
			}
		case "h", "help":
			{
				viewViewHelp(roomID.String())
			}
		default:
			{
				viewViewHelp(roomID.String())
			}
		}
	}
}

func writingEmail(evt *event.Event, message string) {
	roomID := evt.RoomID
	writeTemp, err := getWritingTemp(string(roomID))
	if err != nil {
		WriteLog(critical, "#43 getWritingTemp: "+err.Error())
		matrixClient.SendText(roomID, "An server-error occured Errorcode: #43")
		deleteWritingTemp(string(roomID))
		return
	}
	if len(strings.Trim(writeTemp.subject, " ")) == 0 {
		if evt.Content.AsMessage().MsgType != event.MsgText {
			matrixClient.SendText(roomID, "You have to send a text for subject!")
			return
		}
		err = saveWritingtemp(string(roomID), "subject", message)
		if err != nil {
			WriteLog(critical, "#44 saveWritingtemp: "+err.Error())
			matrixClient.SendText(roomID, "An server-error occured Errorcode: #44")
			deleteWritingTemp(string(roomID))
			return
		}
		matrixClient.SendText(roomID, "Now send me the content of the email. One message is one line. If you want to send or cancel enter !send or !cancel")
	} else {
		if message == "!send" {
			account, err := getSMTPAccount(string(roomID))
			if err != nil {
				WriteLog(critical, "#52 saveWritingtemp: "+err.Error())
				matrixClient.SendText(roomID, "An server-error occured Errorcode: #52")
				deleteWritingTemp(string(roomID))
				return
			}

			m := gomail.NewMessage()
			m.SetHeader("From", account.username)

			if strings.Contains(writeTemp.receiver, ",") {
				recEmails := strings.Split(writeTemp.receiver, ",")
				m.SetHeader("To", recEmails...)
			} else {
				m.SetHeader("To", writeTemp.receiver)
			}

			m.SetHeader("Subject", writeTemp.subject)

			if writeTemp.markdown {
				toSendText := string(markdown.ToHTML([]byte(writeTemp.body), nil, nil))
				toSendText = strings.ReplaceAll(toSendText, "\r\n<h", "<h")
				toSendText = strings.ReplaceAll(toSendText, "\n\n<h", "<h")
				toSendText = strings.ReplaceAll(toSendText, ">\n\n", ">")
				toSendText = strings.ReplaceAll(toSendText, "\r\n", "<br>")
				m.SetBody("text/html", toSendText)

				plainbody := writeTemp.body
				plainbody = strings.ReplaceAll(plainbody, "<br>", "\r\n")
				m.AddAlternative("text/plain", plainbody)
			} else {
				m.SetBody("text/plain", writeTemp.body)
			}

			attachments, err := getAttachments(writeTemp.pkID)
			if err == nil {
				for _, i := range attachments {
					matrixClient.SendText(roomID, "Attaching file: "+i)
					m.Attach(tempDir + i)
				}
			} else {
				matrixClient.SendText(roomID, "coulnd't attach files: "+err.Error())
			}

			d := gomail.NewDialer(account.host, account.port, account.username, account.password)
			if account.ignoreSSL {
				d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
			}
			matrixClient.SendText(roomID, "Sending...")
			if err := d.DialAndSend(m); err != nil {
				WriteLog(logError, "#46 DialAndSend: "+err.Error())
				matrixClient.SendText(roomID, "An server-error occured Errorcode: #53\r\n"+err.Error())
				removeSMTPAccount(string(roomID))
				matrixClient.SendText(roomID, "To fix this errer you have to run !setup smtp .... again")
				deleteWritingTemp(string(roomID))
				return
			}
			matrixClient.SendText(roomID, "Message sent successfully")
			deleteWritingTemp(string(roomID))
		} else if message == "!cancel" {
			matrixClient.SendText(roomID, "Mail canceled")
			deleteWritingTemp(string(roomID))
			return
		} else if strings.HasPrefix(message, "!rm") && len(strings.Split(message, " ")) > 0 {
			splitted := strings.Split(message, " ")[1:]
			var fileName string
			for _, a := range splitted {
				fileName += a + " "
			}
			fileName = strings.TrimRight(fileName, " ")
			fileName = strings.TrimLeft(fileName, " ")
			fmt.Println(fileName)
			err := deleteAttachment(fileName, writeTemp.pkID)
			if err != nil {
				matrixClient.SendText(roomID, "Couldn't delete attachment: "+err.Error())
				return
			}
			_ = os.Remove(tempDir + fileName)
			matrixClient.SendText(roomID, "Attachment deleted!")

		} else {
			if evt.Content.AsMessage().MsgType == event.MsgText {
				if len(strings.ReplaceAll(writeTemp.body, " ", "")) == 0 {
					err = saveWritingtemp(string(roomID), "body", message+"\r\n")
				} else {
					err = saveWritingtemp(string(roomID), "body", writeTemp.body+message+"\r\n")
				}
				if err != nil {
					WriteLog(critical, "#54 saveWritingtemp: "+err.Error())
					matrixClient.SendText(roomID, "An server-error occured Errorcode: #54")
					deleteWritingTemp(string(roomID))
					return
				}
			} else if evt.Content.AsMessage().MsgType == event.MsgFile || evt.Content.AsMessage().MsgType == event.MsgImage {
				if strings.HasPrefix(string(evt.Content.AsMessage().URL), "mxc://") {
					reader, err := matrixClient.Download(id.MustParseContentURI(evt.Content.AsMessage().Body))
					if err != nil {
						matrixClient.SendText(roomID, "Couldn't download File: "+err.Error())
					} else {
						filename := strconv.Itoa(int(time.Now().Unix())) + "_" + evt.Content.AsMessage().Body
						err := streamToTempFile(reader, filename)
						if err != nil {
							matrixClient.SendText(roomID, "Couldn't download file: "+err.Error())
						} else {
							addEmailAttachment(writeTemp.pkID, filename)
							matrixClient.SendText(roomID, "File "+filename+" attached!")
						}
					}
				}
			}

		}
	}
}

func runCommand(message string, evt *event.Event) {
	if strings.HasPrefix(message, "!") {
		firstWord, restOfMesage, _ := strings.Cut(message, " ")
		commandHandler, ok := commands[firstWord]
		if ok {
			commandHandler(evt, restOfMesage)
		} else {
			matrixClient.SendText(evt.RoomID, "command not found!")
		}
	}
}
