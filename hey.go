// Copyright 2014 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Command hey is an HTTP load generator.
package main

import (
	"os"
	"os/signal"
	"time"
)

const (
	heyUA = "hey/0.0.1"
)

func main() {
	config, err := parseFlags()
	if err != nil {
		usageAndExit(err.Error())
	}

	w, err := newWork(config)
	if err != nil {
		usageAndExit(err.Error())
	}

	w.Init()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		w.Stop()
	}()
	if config.dur > 0 {
		go func() {
			time.Sleep(config.dur)
			w.Stop()
		}()
	}
	w.Run()
}
