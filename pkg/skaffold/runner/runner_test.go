/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package runner

import (
	"bytes"
	"io"
	"testing"

	"fmt"

	"github.com/GoogleCloudPlatform/skaffold/pkg/skaffold/build"
	"github.com/GoogleCloudPlatform/skaffold/pkg/skaffold/build/tag"
	"github.com/GoogleCloudPlatform/skaffold/pkg/skaffold/config"
	"github.com/GoogleCloudPlatform/skaffold/pkg/skaffold/constants"
	"github.com/GoogleCloudPlatform/skaffold/pkg/skaffold/deploy"
	"github.com/GoogleCloudPlatform/skaffold/pkg/skaffold/watch"
	"github.com/GoogleCloudPlatform/skaffold/testutil"
	"github.com/sirupsen/logrus"
)

type TestBuilder struct {
	res *build.BuildResult
	err error
}

type TestDeployer struct {
	res *deploy.Result
	err error
}

func (t *TestBuilder) Run(io.Writer, tag.Tagger) (*build.BuildResult, error) {
	return t.res, t.err
}

type TestWatcher struct {
	res []*watch.Event
	err error

	current int
}

func NewTestWatch(err error, res ...*watch.Event) *TestWatcher {
	return &TestWatcher{res: res, err: err}
}

func (t *TestWatcher) Watch(artifacts []*config.Artifact, ready chan *watch.Event, cancel chan struct{}) (*watch.Event, error) {
	if t.current > len(t.res)-1 {
		logrus.Fatalf("Called watch too many times. Events %d, Current: %d", len(t.res)-1, t.current)
	}
	ret := t.res[t.current]
	t.current = t.current + 1
	return ret, t.err
}

func (t *TestDeployer) Run(*build.BuildResult) (*deploy.Result, error) {
	return t.res, t.err
}
func TestNewForConfig(t *testing.T) {
	var tests = []struct {
		description string
		config      *config.SkaffoldConfig
		shouldErr   bool
		sendCancel  bool
		expected    interface{}
	}{
		{
			description: "local builder config",
			config: &config.SkaffoldConfig{
				Build: config.BuildConfig{
					TagPolicy: constants.TagStrategySha256,
					BuildType: config.BuildType{
						LocalBuild: &config.LocalBuild{},
					},
				},
				Deploy: config.DeployConfig{
					DeployType: config.DeployType{
						KubectlDeploy: &config.KubectlDeploy{},
					},
				},
			},
			expected: &build.LocalBuilder{},
		},
		{
			description: "bad tagger config",
			config: &config.SkaffoldConfig{
				Build: config.BuildConfig{
					BuildType: config.BuildType{
						LocalBuild: &config.LocalBuild{},
					},
				},
				Deploy: config.DeployConfig{
					DeployType: config.DeployType{
						KubectlDeploy: &config.KubectlDeploy{},
					},
				},
			},
			shouldErr: true,
		},
		{
			description: "unknown builder",
			config: &config.SkaffoldConfig{
				Build: config.BuildConfig{},
			},
			shouldErr: true,
			expected:  &build.LocalBuilder{},
		},
		{
			description: "unknown tagger",
			config: &config.SkaffoldConfig{
				Build: config.BuildConfig{
					TagPolicy: "bad tag strategy",
					BuildType: config.BuildType{
						LocalBuild: &config.LocalBuild{},
					},
				}},
			shouldErr: true,
			expected:  &build.LocalBuilder{},
		},
		{
			description: "unknown deployer",
			config: &config.SkaffoldConfig{
				Build: config.BuildConfig{
					TagPolicy: constants.TagStrategySha256,
					BuildType: config.BuildType{
						LocalBuild: &config.LocalBuild{},
					},
				},
				Deploy: config.DeployConfig{},
			},
			shouldErr: true,
		},
		{
			description: "nil deployer",
			config: &config.SkaffoldConfig{
				Build: config.BuildConfig{
					TagPolicy: constants.TagStrategySha256,
					BuildType: config.BuildType{
						LocalBuild: &config.LocalBuild{},
					},
				},
			},
			shouldErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			cfg, err := NewForConfig(&bytes.Buffer{}, false, test.config)
			testutil.CheckError(t, test.shouldErr, err)
			if cfg != nil {
				testutil.CheckErrorAndTypeEquality(t, test.shouldErr, err, test.expected, cfg.Builder)
			}
		})
	}
}

func TestRun(t *testing.T) {
	var tests = []struct {
		description string
		runner      *SkaffoldRunner
		devmode     bool
		shouldErr   bool
	}{
		{
			description: "run no error",
			runner: &SkaffoldRunner{
				config: &config.SkaffoldConfig{},
				Builder: &TestBuilder{
					res: &build.BuildResult{},
					err: nil,
				},
				devMode: false,
				Tagger:  &tag.ChecksumTagger{},
				Deployer: &TestDeployer{
					res: &deploy.Result{},
					err: nil,
				},
			},
		},
		{
			description: "run build error",
			runner: &SkaffoldRunner{
				config: &config.SkaffoldConfig{},
				Builder: &TestBuilder{
					err: fmt.Errorf(""),
				},
				Tagger: &tag.ChecksumTagger{},
			},
			shouldErr: true,
		},
		{
			description: "run deploy error",
			runner: &SkaffoldRunner{
				Deployer: &TestDeployer{
					err: fmt.Errorf(""),
				},
				Tagger: &tag.ChecksumTagger{},
				Builder: &TestBuilder{
					res: &build.BuildResult{},
					err: nil,
				},
			},
			shouldErr: true,
		},
		{
			description: "run dev mode",
			runner: &SkaffoldRunner{
				config:     &config.SkaffoldConfig{},
				Builder:    &TestBuilder{},
				Deployer:   &TestDeployer{},
				Watcher:    NewTestWatch(nil, &watch.Event{}, watch.WatchStopEvent),
				devMode:    true,
				cancel:     make(chan struct{}, 1),
				watchReady: make(chan *watch.Event, 1),
				Tagger:     &tag.ChecksumTagger{},
			},
		},
		{
			description: "run dev mode build error",
			runner: &SkaffoldRunner{
				config: &config.SkaffoldConfig{},
				Builder: &TestBuilder{
					err: fmt.Errorf(""),
				},
				Watcher:    NewTestWatch(nil, &watch.Event{}, watch.WatchStopEvent),
				devMode:    true,
				cancel:     make(chan struct{}, 1),
				watchReady: make(chan *watch.Event, 1),
				Tagger:     &tag.ChecksumTagger{},
			},
		},
		{
			description: "bad watch dev mode",
			runner: &SkaffoldRunner{
				config: &config.SkaffoldConfig{},
				Builder: &TestBuilder{
					res: &build.BuildResult{},
					err: nil,
				},
				Deployer:   &TestDeployer{},
				Watcher:    NewTestWatch(fmt.Errorf(""), nil),
				devMode:    true,
				cancel:     make(chan struct{}, 1),
				watchReady: make(chan *watch.Event, 1),
				Tagger:     &tag.ChecksumTagger{},
			},
			shouldErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			err := test.runner.Run()
			testutil.CheckError(t, test.shouldErr, err)
		})
	}
}