package logrus_pushover

import (
	"errors"
	"fmt"
	"github.com/gregdel/pushover"
	log "github.com/sirupsen/logrus"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// throttling parameters
//
// We want to slow down messages if they are bursting - to avoid flood
// we will allow up to NOMUTECOUNT messages within the TIMEFRAME, then we will start sending out messages at a slower rate.
// after we had some quite period (i.e. the message coming is more than MUTEDELAY after the last one) we will reset the counter.

const MUTERESETDELAY = time.Second * 60 // after such quite period throttling will be canceled
const MUTEDELAY = 5 * time.Second       // default delay between two msg sent to pushover (to avoid flood)
const NOMUTECOUNT = 5                   // maximum burst allowed

// number of messages to queue, if the queue is full an error is produced and the entry is discarded

/*
The pushover service is based on REST API, thus it should be called in an async way.  There is also a monthly
limit, so a flood of messages should be avoided.

After constructing the hook, you may modify the above numbers using the throttle call

*/

// this Queue type is used to limit the rate of messages sent to pushover
// if the channel is full - messages are discarded and an error is produced

const QLEN = 100

type Queue struct {
	lEntry *log.Entry
	tm     time.Time
}

var qChannel = make(chan Queue, QLEN)
var once sync.Once
var sigs = make(chan os.Signal, 1)

// PushoverHook sends log via Pushover (https://pushover.net/)
type PushoverHook struct {
	async bool // set to true not to wait for the http sent to pushover
	// to avoid flood, hook will wait muteDelay between two msg sent to pushover
	//muteDelay     time.Duration // default is 1 minute
	//noMuteCount   int           // how many messages to send without throttling
	burstCount    int           // number of messages sent within the muteDelay (if this reach noMuteCount - we should throttle)
	lastMsgSentAt time.Time     // last msg sent to pushover
	logLevel      log.Level     // max log level to use default is ErrorLevel.
	formatter     log.Formatter // the formatter to use to format the msg sent to pushover - note that a special version exit for pushover
	// below created on when the hook is created
	pushOverApp       *pushover.Pushover
	PushOverRecipient *pushover.Recipient
	deviceName        string
	muteReset         time.Duration
	muteDelay         time.Duration
	maxBurstCount     int
	useFixedFont      bool
}

// NewPushoverHook init & returns a new PushoverHook
// specify user & app tokens, set async to true to avoid waiting for pushover and set min log level.  Device name will only send to specified device
// set fixedFont to use the fixed font, otherwise html is used
func NewPushoverHook(pushoverUserToken, pushoverAPIToken string, async bool, logLevel log.Level, device string, fixedFont bool) *PushoverHook {
	p := PushoverHook{
		async:      async,
		burstCount: 0,
		logLevel:   logLevel, // default is Error Level
		deviceName: device,

		formatter: &prefixed.TextFormatter{
			ForceFormatting: true,
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
			DisableColors:   true},
		muteReset:     MUTERESETDELAY,
		muteDelay:     MUTEDELAY,
		maxBurstCount: NOMUTECOUNT,
		useFixedFont:  fixedFont,
	}
	p.pushOverApp = pushover.New(pushoverAPIToken)
	p.PushOverRecipient = pushover.NewRecipient(pushoverUserToken)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	return &p
}

// Throttle - modify the throttle paramters:
// muteReset - Time without messages to reset any throttle
// muteDelay - Time to delay messages if they are flooding
// burstCount - Allowed burst count before throttling
func (hook *PushoverHook) Throttle(muteReset, muteDelay time.Duration, maxBurstCount int) {
	hook.muteReset = muteReset
	hook.muteDelay = muteDelay
	hook.maxBurstCount = maxBurstCount
}

// Levels returns the available logging levels.
func (hook *PushoverHook) Levels() []log.Level {
	return log.AllLevels[:hook.logLevel+1]

}

// the actual routine to send the message to pushover
func (hook *PushoverHook) SendMessage(entry *log.Entry, delay time.Duration) {
	var p int
	hook.lastMsgSentAt = time.Now()

	if delay > 0 {
		fmt.Println("sleeping for ", delay.Seconds(), " seconds")
		time.Sleep(delay)
	}
	msgString, _ := hook.formatter.Format(entry)
	//fmt.Printf("msgString: [%s]\n", msgString)
	switch entry.Level {
	case log.PanicLevel:
		p = 2
	case log.FatalLevel:
		p = 1
	case log.ErrorLevel:
		p = 0
	case log.WarnLevel:
		p = -1
	case log.InfoLevel:
		p = -2
	default:
		p = -2
	}
	msg := &pushover.Message{
		Message: string(msgString), // "<b><h1>Message Body</h1></b> <br> <i>Message Body in italic</i>",
		//Title:       "Message Title",
		Priority:    p,
		Timestamp:   entry.Time.Unix(),
		Expire:      time.Hour,
		CallbackURL: "",
		DeviceName:  hook.deviceName,
		//Sound:       pushover.SoundBike,
		HTML:      !hook.useFixedFont,
		Monospace: hook.useFixedFont,
	}
	//fmt.Printf("sending to pushover: @%s : %s", time.Now().Format("2006-01-02 15:04:05.000000"), msg.Message)
	f := func() {
		_, err := hook.pushOverApp.SendMessage(msg, hook.PushOverRecipient)
		if err != nil {
			fmt.Println("pushover sending error:", err)
			//fmt.Printf("msg.message=[%s]\n", msg.Message)
			//fmt.Printf("msg.priority=[%d]\n", msg.Priority)
			//fmt.Printf("msg.devicename=[%s]\n", msg.DeviceName)
			//fmt.Printf("msg.html=[%t]\n", msg.HTML)
			//fmt.Printf("msg.monospace=[%t]\n", msg.Monospace)
			//fmt.Printf("msg.Title=[%s]\n", msg.Title)
			// give it some time....
			time.Sleep(100 * time.Millisecond)
		}
	}
	if hook.async {
		go f()
	} else {
		f()
	}

}

// Fire is called when a log event is fired.  all messages are sent via a goroutine to avoid the wait
// note that if we are flooded a delay is added to each message.
func (hook *PushoverHook) Fire(entry *log.Entry) error {
	//var waiting time.Duration
	if entry.Level > hook.logLevel {
		return nil
	}
	// if queue is full, discard the message
	if len(qChannel) == cap(qChannel) {
		fmt.Println("queue is full, discarding message")
		return errors.New("queue is full, discarding message")
	}

	qChannel <- Queue{entry, time.Now()}
	// run the queue processor only once
	go func() {
		once.Do(hook.ProcessQueue)
	}()
	return nil
}

func (hook *PushoverHook) ProcessQueue() {
	//fmt.Println("starting queue processor")
	for {
		select {
		case s := <-sigs:
			fmt.Println("Exit/Kil Signal - exiting", s)
			return
		case q := <-qChannel:
			d := calcDelay(hook)
			//fmt.Println("Got message from queue:", q.lEntry.Message, "  delay:", d)
			hook.SendMessage(q.lEntry, d)

		}
	}
}

// Calculate the delay to add to the message in case of flooding
// we allow a certain burst of messages, once we reach the burst limit, we add a delay (MUTEDELAY) to each message.
// if there were no messages for MUTERESETDELAY, we re-allow bursts.
func calcDelay(h *PushoverHook) time.Duration {
	tsince := time.Since(h.lastMsgSentAt)
	//fmt.Printf("LastMsg: %s time Since last: %f, burstCounter=%d\n", h.lastMsgSentAt.Format(time.StampMilli), tsince.Seconds(), h.burstCount)
	// first check if we had a nice delay from previous message, then reset the burst counter
	if tsince >= h.muteReset {
		h.burstCount = 0
		//h.lastMsgSentAt = time.Now()
		return 0 // no delay
	}
	// if the message is close to the previous one, we increase the burst counter and check if we need to delay
	if tsince < h.muteDelay {
		h.burstCount++
		// below the burst limit, no delay
		if h.burstCount < h.maxBurstCount {
			return 0
		} else {
			// we reached the burst limit, we delay the message but reduce the burst counter.
			h.burstCount--
			return h.muteDelay
		}
	}
	// if we are here, we are not flooding, we reduce  the burst counter and return no delay
	if h.burstCount > 0 {
		h.burstCount--
	}
	return 0
}