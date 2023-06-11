# Logrus Pushover Package

A simple wrapper for pushover to use with Logrus, the structured logger for Go.
Allows logging messages to be sent over to Pushover service (https://pushover.net/).
The user has full control over the log level to be sent to pushover, and the rate of messages sent.


## Installation

Please create an API key and user key in Pushover - you need them to send the messages


### Sample usage

```go
package main

import (
	"fmt"
	"github.com/razsteinmetz/logrus_pushover"
	"github.com/sirupsen/logrus"
	"time"
)

// setup your api key and user key
const apikey = "lognapikey"
const userkey = "long user key"

func main() {
	// note different log level to pushover (Error level) vs Debug level to console
	hook := logrus_pushover.NewPushoverHook(userkey, apikey1, true, logrus.ErrorLevel, "", true)
	log := logrus.New()
	log.Hooks.Add(hook)
	log.SetFormatter(&logrus.TextFormatter{
		ForceColors: true,
	})
	// sample messages sent to pushover and console
	log.WithFields(logrus.Fields{"fied1": "1", "field2": "2"}).Error(msg)
	log.WithFields(logrus.Fields{"fied1": "2", "field2": "4"}).Debug(msg)
	log.WithFields(logrus.Fields{"fied1": "3", "field2": "5"}).Info(msg)
	log.Debugln("Debugln message")
	log.Infoln("infoln message")
	log.Errorln("errorln message")
	// give some time for messages to be sent to pushover
	for {
		time.Sleep(5 * time.Second)
	}
}

```