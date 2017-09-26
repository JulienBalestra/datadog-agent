// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2017 Datadog, Inc.

package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/DataDog/datadog-agent/cmd/agent/common"
	"github.com/DataDog/datadog-agent/pkg/aggregator"
	"github.com/DataDog/datadog-agent/pkg/collector/check"
	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/DataDog/datadog-agent/pkg/serializer"
	"github.com/DataDog/datadog-agent/pkg/status"
	"github.com/DataDog/datadog-agent/pkg/tagger"
	"github.com/DataDog/datadog-agent/pkg/util"
	"github.com/spf13/cobra"
)

var (
	checkRate  bool
	checkName  string
	checkDelay int
	logLevel   string
)

const checkCmdFlushInterval = 10000000000

func init() {
	AgentCmd.AddCommand(checkCmd)

	checkCmd.Flags().StringVarP(&confFilePath, "cfgpath", "f", "", "path to directory containing datadog.yaml")
	checkCmd.Flags().BoolVarP(&checkRate, "check-rate", "r", false, "check rates by running the check twice")
	checkCmd.Flags().StringVarP(&logLevel, "log-level", "l", "", "set the log level (default 'off')")
	checkCmd.Flags().IntVarP(&checkDelay, "delay", "d", 100, "delay between running the check and grabbing the metrics in miliseconds")
	checkCmd.SetArgs([]string{"checkName"})
}

var checkCmd = &cobra.Command{
	Use:   "check <check_name>",
	Short: "Run the specified check",
	Long:  `Use this to run a specific check with a specific rate`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Global Agent configuration
		err := common.SetupConfig(confFilePath)
		if err != nil {
			fmt.Printf("Cannot setup config, exiting: %v\n", err)
			return err
		}

		if logLevel == "" {
			if confFilePath != "" {
				logLevel = config.Datadog.GetString("log_level")
			} else {
				logLevel = "off"
			}
		}

		// Setup logger
		err = config.SetupLogger(logLevel, "", "", false, false, "")
		if err != nil {
			fmt.Printf("Cannot setup logger, exiting: %v\n", err)
			return err
		}

		if len(args) != 0 {
			checkName = args[0]
		} else {
			cmd.Help()
			return nil
		}

		hostname, err := util.GetHostname()
		if err != nil {
			fmt.Printf("Cannot get hostname, exiting: %v\n", err)
			return err
		}

		// start tagging system for containers
		err = tagger.Init()
		if err != nil {
			fmt.Printf("Unable to start tagging system: %s", err)
			return err
		}

		s := &serializer.Serializer{Forwarder: common.Forwarder}
		agg := aggregator.InitAggregatorWithFlushInterval(s, hostname, 10000000000)
		common.SetupAutoConfig(config.Datadog.GetString("confd_path"))
		cs := common.AC.GetChecksByName(checkName)
		if len(cs) == 0 {
			fmt.Println("no check found")
			return fmt.Errorf("no check found")
		}

		if len(cs) > 1 {
			fmt.Println("Multiple check instances found, running each of them")
		}

		for _, c := range cs {
			s := runCheck(c, agg)

			// Without a small delay some of the metrics will not show up
			time.Sleep(time.Duration(checkDelay) * time.Millisecond)

			getMetrics(agg)

			checkStatus, _ := status.GetCheckStatus(c, s)
			fmt.Println(string(checkStatus))
		}

		return nil
	},
}

func runCheck(c check.Check, agg *aggregator.BufferedAggregator) *check.Stats {
	s := check.NewStats(c)
	i := 0
	times := 1
	if checkRate {
		times = 2
	}
	for i < times {
		t0 := time.Now()
		err := c.Run()
		warnings := c.GetWarnings()
		mStats, _ := c.GetMetricStats()
		s.Add(time.Since(t0), err, warnings, mStats)
		i++
	}

	return s
}

func getMetrics(agg *aggregator.BufferedAggregator) {
	series := agg.GetSeries()
	if len(series) != 0 {
		fmt.Println("Series: ")
		j, _ := json.MarshalIndent(series, "", "  ")
		fmt.Println(string(j))
	}

	sketches := agg.GetSketches()
	if len(sketches) != 0 {
		fmt.Println("Sketches: ")
		j, _ := json.MarshalIndent(sketches, "", "  ")
		fmt.Println(string(j))
	}

	serviceChecks := agg.GetServiceChecks()
	if len(serviceChecks) != 0 {
		fmt.Println("Service Checks: ")
		j, _ := json.MarshalIndent(serviceChecks, "", "  ")
		fmt.Println(string(j))
	}

	events := agg.GetEvents()
	if len(events) != 0 {
		fmt.Println("Events: ")
		j, _ := json.MarshalIndent(events, "", "  ")
		fmt.Println(string(j))
	}
}
