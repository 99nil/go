// Copyright © 2022 zc2638 <zc2638@qq.com>.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package regular

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/99nil/gopkg/logger"
)

type TaskFunc func(context.Context) error

func (tf TaskFunc) Run(ctx context.Context) error {
	return tf(ctx)
}

type TaskInterface interface {
	Run(ctx context.Context) error
}

func New(cfg *Config) (*Engine, error) {
	return NewWithLogger(cfg, nil)
}

func NewWithLogger(cfg *Config, log logger.UniversalInterface) (*Engine, error) {
	e := &Engine{}
	if err := e.SetConfig(cfg); err != nil {
		return nil, err
	}
	if log == nil {
		log = logger.NewEmpty()
	}
	e.log = log
	return e, nil
}

type Engine struct {
	m sync.Mutex

	cfg *Config
	log logger.UniversalInterface

	cancel context.CancelFunc
	stopCh chan struct{}
}

func (e *Engine) SetConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	for k, v := range cfg.Periods {
		if err := v.Parse(); err != nil {
			return fmt.Errorf("analysis time period %d failed: %v", k, err)
		}
	}
	if cfg.Name == "" {
		cfg.Name = "regular"
	}

	e.m.Lock()
	defer e.m.Unlock()
	e.cfg = cfg
	return nil
}

func (e *Engine) GetConfig() *Config {
	e.m.Lock()
	defer e.m.Unlock()
	return e.cfg
}

func (e *Engine) Shutdown() {
	close(e.stopCh)
}

func (e *Engine) Start(ctx context.Context, task TaskInterface) error {
	e.stopCh = make(chan struct{})

	for {
		second := time.Now().Second()
		if second == 0 {
			break
		}
		sleepInterval := 60 - second
		e.log.Warnf("[%s] The current seconds is not 0, need to wait for %ds to start the automatic assistant", e.cfg.Name, sleepInterval)
		time.Sleep(time.Duration(sleepInterval) * time.Second)
	}

	if len(e.GetConfig().Periods) == 0 {
		return e.run(ctx, task)
	}

	currentStartHour, currentStartMinute := -1, -1
	ticker := time.NewTicker(time.Minute)
	for {
		if e.cancel == nil {
			e.log.Debugf("[%s] Start mission reconnaissance", e.cfg.Name)
		}
		now := time.Now()
		hour := now.Hour()
		minute := now.Minute()

		for _, v := range e.GetConfig().Periods {
			if currentStartHour > -1 && (currentStartHour != v.startHour || currentStartMinute != v.startMinute) {
				continue
			}

			start, end := CheckTime(v.startHour, v.startMinute, v.endHour, v.endMinute, hour, minute)
			if start && !end && currentStartHour != v.startHour {
				currentStartHour = v.startHour
				currentStartMinute = v.startMinute

				ctx, e.cancel = context.WithCancel(ctx)
				go func() {
					if err := e.run(ctx, task); err != nil {
						e.log.Errorf("[%s] Execution ends with error: %v", e.cfg.Name, err)
					}
					e.log.Debugf("[%s] The execution of the current time period is over, please wait for the next time period", e.cfg.Name)
				}()
				break
			}
			if start && end && e.cancel != nil {
				e.cancel()
				e.cancel = nil
			}
		}

		select {
		case <-e.stopCh:
			if e.cancel != nil {
				e.cancel()
			}
			e.log.Debugf("[%s] task stopped", e.cfg.Name)
			return nil
		case <-ticker.C:
		}
	}
}

func (e *Engine) run(ctx context.Context, task TaskInterface) error {
	cfg := e.GetConfig()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := task.Run(ctx); err != nil {
			if cfg.FailInterval < 0 {
				return err
			}
			e.log.Errorf("[%s] Execution ends with error: %v", e.cfg.Name, err)
			e.log.Warnf("[%s] Will continue after %dms", e.cfg.Name, cfg.FailInterval)
			fmt.Println()
			time.Sleep(time.Duration(cfg.FailInterval) * time.Millisecond)
			continue
		}
		cfg = e.GetConfig()

		if cfg.SuccessInterval < 0 {
			return nil
		}
		e.log.Debugf("[%s] Executed successfully, will continue after %dms", e.cfg.Name, cfg.SuccessInterval)
		time.Sleep(time.Duration(cfg.SuccessInterval) * time.Millisecond)
	}
}

func CheckTime(startHour, startMinute, endHour, endMinute, currentHour, currentMinute int) (start bool, end bool) {
	startTime := time.Date(0, 0, 0, startHour, startMinute, 0, 0, time.Local)
	endTime := time.Date(0, 0, 0, endHour, endMinute, 0, 0, time.Local)
	currentTime := time.Date(0, 0, 0, currentHour, currentMinute, 0, 0, time.Local)

	since := startTime.Sub(endTime)
	if since == 0 {
		start = true
		return
	} else if since < 0 {
		if currentTime.Sub(startTime) >= 0 {
			start = true
		}
		if currentTime.Sub(endTime) >= 0 {
			end = true
		}
	} else {
		if currentTime.Sub(endTime) >= 0 {
			end = true
		}
		if currentTime.Sub(startTime) >= 0 || !end {
			start = true
			end = false
		}
	}
	return
}
