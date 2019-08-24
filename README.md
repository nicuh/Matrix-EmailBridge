# Matrix-EmailBot
A matrix-bridge written in GOlang to let you read your emails in matrix


## Information
This bot is currently in development. Its not 100% tested</code>

## Install
Just run <code>go get</code> to fetch the required dependencies and <code>go build</code> inside of the folder and execute the created binary. Then you have to adjust the config file to make it work with your matrix server.
Invite your bot into a private room, it will join automatically. If everyting is set up correctly, you can bridge the room by typing !login. Then you just have to follow the instructions. Typing !help shows a list with available commands.<br>Creating a new private room with the bot lets you add a differen email account.<br>

## Note
Note: you should change the permissions of the <code>cfg.json</code> and <code>data.db</code> to <b>640</b> or <b>660</b> because they contain sensitive data, not every user should be able to read them!

## Features
- [X]  Receiving Email with IMAPs
- [X]  Use custom IMAPs Server and port
- [X]  Use the bot with multiple email addresses
- [X]  Use the bot with multiple user
- [ ]  Use custom mailbox instead of INBOX
- [ ]  Send email
- [ ]  Use commands to move/delete emails
