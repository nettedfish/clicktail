// Package mongodb is a parser for mongodb logs
package mongodb

import (
	"errors"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/honeycombio/mongodbtools/logparser"

	"github.com/honeycombio/honeytail/event"
)

const (
	ctimeTimeFormat        = "Mon Jan _2 15:04:05.000"
	ctimeNoMSTimeFormat    = "Mon Jan _2 15:04:05"
	iso8601UTCTimeFormat   = "2006-01-02T15:04:05Z"
	iso8601LocalTimeFormat = "2006-01-02T15:04:05.999999999-0700"
)

var timestampFormats = []string{iso8601LocalTimeFormat, iso8601UTCTimeFormat, ctimeNoMSTimeFormat, ctimeTimeFormat}

type Options struct {
	LogPartials bool `long:"log_partials" description:"Send what was successfully parsed from a line (only if the error occured in the log line's message)."`
}

type Parser struct {
	conf       Options
	lineParser LineParser
	nower      Nower
}

type LineParser interface {
	ParseLogLine(line string) (map[string]interface{}, error)
}

type MongoLineParser struct {
}

func (m *MongoLineParser) ParseLogLine(line string) (map[string]interface{}, error) {
	return logparser.ParseLogLine(line)
}

func (p *Parser) Init(options interface{}) error {
	p.conf = *options.(*Options)
	p.nower = &RealNower{}
	p.lineParser = &MongoLineParser{}
	return nil
}

func (p *Parser) parseTimestamp(values map[string]interface{}) (time.Time, error) {
	now := p.nower.Now()
	timestamp_value, ok := values["timestamp"].(string)
	if ok {
		var err error
		for _, f := range timestampFormats {
			timestamp, err := time.Parse(f, timestamp_value)
			if err == nil {
				if f == ctimeTimeFormat || f == ctimeNoMSTimeFormat {
					// these formats lacks the year, so we check
					// if adding Now().Year causes the date to be
					// after today.  if it's after today, we
					// decrement year by 1.  if it's not after, we
					// use it.
					ts := timestamp.AddDate(now.Year(), 0, 0)
					if now.After(ts) {
						return ts, nil
					}

					return timestamp.AddDate(now.Year()-1, 0, 0), nil
				}
				return timestamp, nil
			}
		}
		return time.Time{}, err
	}

	return time.Time{}, errors.New("timestamp missing from logline")
}

func (p *Parser) ProcessLines(lines <-chan string, send chan<- event.Event) {
	for line := range lines {
		values, err := p.lineParser.ParseLogLine(line)
		// we get a bunch of errors from the parser on mongo logs, skip em
		if err == nil || (p.conf.LogPartials && logparser.IsPartialLogLine(err)) {
			timestamp, err := p.parseTimestamp(values)
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"line": line,
				}).WithError(err).Debug("couldn't parse logline timestamp, skipping.")
				continue
			}
			logrus.WithFields(logrus.Fields{
				"line":   line,
				"values": values,
			}).Debug("Successfully parsed line")

			// we'll be putting the timestamp in the Event
			// itself, no need to also have it in the Data
			delete(values, "timestamp")

			send <- event.Event{
				Timestamp: timestamp,
				Data:      values,
			}
		} else {
			logrus.WithFields(logrus.Fields{
				"line": line,
			}).WithError(err).Debug("logline didn't parse, skipping.")
		}
	}
	logrus.Debug("lines channel is closed, ending mongo processor")
}

type Nower interface {
	Now() time.Time
}

type RealNower struct{}

func (r *RealNower) Now() time.Time {
	return time.Now().UTC()
}
