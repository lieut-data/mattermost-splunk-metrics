package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/textparse"
	"github.com/sirupsen/logrus"
)

func main() {
	scrapeTarget := flag.String("scrape_target", "http://localhost:8067/metrics", "prometheus exporter to scrape")
	scrapeInterval := flag.Duration("scrape_interval", 15*time.Second, "interval on which to scrape")
	splunkTarget := flag.String("splunk_target", "", "splunk hostname")
	splunkAuthorization := flag.String("splunk_authorization", "", "token to authorize splunk")
	insecure := flag.Bool("allow_insecure", false, "skip certificate verification")
	logLevel := flag.String("log_level", "info", "logrus log level (error, info, debug, ...)")
	timeout := flag.Duration("timeout", 3*time.Second, "http request timeouts")

	flag.Parse()

	logrusLevel, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.WithError(err).Fatal("failed to parse log level")
	}
	logrus.SetLevel(logrusLevel)

	if *splunkTarget == "" {
		logrus.Fatal("invalid splunk target")
	}
	if *splunkAuthorization == "" {
		logrus.Fatal("invalid splunk authorization")
	}

	if *insecure {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	err = run(*scrapeTarget, *scrapeInterval, *splunkTarget, *splunkAuthorization, *timeout)
	if err != nil {
		logrus.WithError(err).Fatal("failed to run")
	}
}

func run(scrapeTarget string, scrapeInterval time.Duration, splunkTarget, splunkAuthorization string, timeout time.Duration) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	hostname, err := os.Hostname()
	if err != nil {
		return errors.Wrap(err, "failed to get hostname")
	}

	ctx := context.Background()
	for {
		scrapeAndSend(ctx, scrapeTarget, hostname, splunkTarget, splunkAuthorization, timeout)

		logrus.WithField("duration", scrapeInterval).Debug("sleeping until next scrape")
		select {
		case <-sigs:
			logrus.Debug("terminating")
			return nil
		case <-time.After(scrapeInterval):
		}
	}
}

func scrapeAndSend(ctx context.Context, scrapeTarget, hostname, splunkTarget, splunkAuthorization string, timeout time.Duration) {
	scrapeCtx, cncl := context.WithTimeout(ctx, timeout)
	defer cncl()
	payload := doScrape(scrapeCtx, scrapeTarget, hostname)
	if payload == nil {
		logrus.Warn("empty scrape, skipping send to splunk")
		return
	}

	splunkCtx, cncl := context.WithTimeout(ctx, timeout)
	defer cncl()
	err := sendToSplunk(splunkCtx, splunkTarget, splunkAuthorization, payload)
	if err != nil {
		logrus.WithError(err).Error("failed to send to splunk")
	}
}

func doScrape(ctx context.Context, scrapeTarget, hostname string) *bytes.Buffer {
	logger := logrus.WithField("scrape_target", scrapeTarget)
	logger.Debug("scraping target")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scrapeTarget, nil)
	if err != nil {
		logger.WithError(err).Warn("failed to create request")
		return nil
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.WithError(err).Warn("failed to get target")
		return nil
	}

	input, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.WithError(err).Warn("failed to read request body")
	}

	logger.WithField("bytes", len(input)).Debug("read body")

	var outputBuffer bytes.Buffer

	parser := textparse.NewPromParser(input)
	for {
		entry, err := parser.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			logger.WithError(err).Error("failed to parse endpoint")
			return nil
		}
		if entry != textparse.EntrySeries {
			continue
		}

		_, _, value := parser.Series()

		var l labels.Labels
		_ = parser.Metric(&l)

		payload := make(map[string]interface{})
		payload["time"] = time.Now().Unix()
		payload["event"] = "metric"
		payload["host"] = hostname
		fields := make(map[string]interface{})

		for _, label := range l {
			if label.Name == "__name__" {
				fields[fmt.Sprintf("metric_name:%s", label.Value)] = value
				continue
			}

			fields[label.Name] = label.Value
		}
		payload["fields"] = fields

		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			logger.WithError(err).Error("failed to marshal payload")
		}

		outputBuffer.Write(payloadJSON)
	}

	return &outputBuffer
}

func sendToSplunk(ctx context.Context, splunkTarget, splunkAuthorization string, payload *bytes.Buffer) error {
	logger := logrus.WithField("splunk_target", splunkTarget)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, splunkTarget, payload)
	if err != nil {
		logger.WithError(err).Warn("failed to create request")
		return nil
	}

	req.Header.Add("Authorization", fmt.Sprintf("Splunk %s", splunkAuthorization))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.WithError(err).Warn("failed to post")
		return nil
	}
	if resp.StatusCode == 200 {
		logger.WithField("bytes", payload.Len()).Debug("successfully posted payload")
	} else {
		bodyError, err := ioutil.ReadAll(resp.Body)
		if err == nil {
			logger.WithField("status", resp.Status).WithField("body", string(bodyError)).Error("unexpected status posting payload")
		} else {
			logger.WithField("status", resp.Status).Error("unexpected status posting payload")
		}
	}

	return nil
}
