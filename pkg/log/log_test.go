/*
Copyright 2018 The Kubernetes Authors.

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

package log

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ logr.LogSink = &delegatingLogSink{}

// logInfo is the information for a particular fakeLogger message.
type logInfo struct {
	name []string
	tags []interface{}
	msg  string
}

// fakeLoggerRoot is the root object to which all fakeLoggers record their messages.
type fakeLoggerRoot struct {
	messages []logInfo
}

// fakeLogger is a fake implementation of logr.Logger that records
// messages, tags, and names,
// just records the name.
type fakeLogger struct {
	name []string
	tags []interface{}

	root *fakeLoggerRoot
}

func (f *fakeLogger) Init(info logr.RuntimeInfo) {
}

func (f *fakeLogger) WithName(name string) logr.LogSink {
	names := append([]string(nil), f.name...)
	names = append(names, name)
	return &fakeLogger{
		name: names,
		tags: f.tags,
		root: f.root,
	}
}

func (f *fakeLogger) WithValues(vals ...interface{}) logr.LogSink {
	tags := append([]interface{}(nil), f.tags...)
	tags = append(tags, vals...)
	return &fakeLogger{
		name: f.name,
		tags: tags,
		root: f.root,
	}
}

func (f *fakeLogger) Error(err error, msg string, vals ...interface{}) {
	tags := append([]interface{}(nil), f.tags...)
	tags = append(tags, "error", err)
	tags = append(tags, vals...)
	f.root.messages = append(f.root.messages, logInfo{
		name: append([]string(nil), f.name...),
		tags: tags,
		msg:  msg,
	})
}

func (f *fakeLogger) Info(l int, msg string, vals ...interface{}) {
	tags := append([]interface{}(nil), f.tags...)
	tags = append(tags, vals...)
	f.root.messages = append(f.root.messages, logInfo{
		name: append([]string(nil), f.name...),
		tags: tags,
		msg:  msg,
	})
}

func (f *fakeLogger) Enabled(l int) bool {
	return true
}

var _ = Describe("logging", func() {

	Describe("top-level logSink", func() {
		It("hold newly created loggers until a logSink is set", func() {
			By("grabbing a new sub-logSink and logging to it")
			l1 := Log.WithName("runtimeLog").WithValues("newtag", "newvalue1")
			l1.Info("before msg")

			By("actually setting the logSink")
			logger := &fakeLogger{root: &fakeLoggerRoot{}}
			SetLogger(logr.New(logger))

			By("grabbing another sub-logSink and logging to both loggers")
			l2 := Log.WithName("runtimeLog").WithValues("newtag", "newvalue2")
			l1.Info("after msg 1")
			l2.Info("after msg 2")

			By("ensuring that messages after the logSink was set were logged")
			Expect(logger.root.messages).To(ConsistOf(
				logInfo{name: []string{"runtimeLog"}, tags: []interface{}{"newtag", "newvalue1"}, msg: "after msg 1"},
				logInfo{name: []string{"runtimeLog"}, tags: []interface{}{"newtag", "newvalue2"}, msg: "after msg 2"},
			))
		})
	})

	Describe("lazy logSink initialization", func() {
		var (
			root     *fakeLoggerRoot
			baseLog  logr.LogSink
			delegLog logr.Logger
		)

		BeforeEach(func() {
			root = &fakeLoggerRoot{}
			baseLog = &fakeLogger{root: root}
			delegLog = NewDelegatingLogger(NullLogSink{})
		})

		It("should delegate with name", func() {
			By("asking for a logSink with a name before fulfill, and logging")
			befFulfill1 := delegLog.WithName("before-fulfill")
			befFulfill2 := befFulfill1.WithName("two")
			befFulfill1.Info("before fulfill")

			By("logging on the base logSink before fulfill")
			delegLog.Info("before fulfill base")

			By("ensuring that no messages were actually recorded")
			Expect(root.messages).To(BeEmpty())

			By("fulfilling the promise")
			delegLog.GetSink().(canFulfill).Fulfill(baseLog)

			By("logging with the existing loggers after fulfilling")
			befFulfill1.Info("after 1")
			befFulfill2.Info("after 2")

			By("grabbing a new sub-logSink of a previously constructed logSink and logging to it")
			befFulfill1.WithName("after-from-before").Info("after 3")

			By("logging with new loggers")
			delegLog.WithName("after-fulfill").Info("after 4")

			By("ensuring that the messages are appropriately named")
			Expect(root.messages).To(ConsistOf(
				logInfo{name: []string{"before-fulfill"}, msg: "after 1"},
				logInfo{name: []string{"before-fulfill", "two"}, msg: "after 2"},
				logInfo{name: []string{"before-fulfill", "after-from-before"}, msg: "after 3"},
				logInfo{name: []string{"after-fulfill"}, msg: "after 4"},
			))
		})

		// This test in itself will always succeed, a failure will be indicated by the
		// race detector going off
		It("should be threadsafe", func() {
			fulfillDone := make(chan struct{})
			withNameDone := make(chan struct{})
			withValuesDone := make(chan struct{})
			grandChildDone := make(chan struct{})
			logEnabledDone := make(chan struct{})
			logInfoDone := make(chan struct{})
			logErrorDone := make(chan struct{})
			logVDone := make(chan struct{})

			// Constructing the child in the goroutine does not reliably
			// trigger the race detector
			child := delegLog.WithName("child")
			go func() {
				defer GinkgoRecover()
				delegLog.GetSink().(canFulfill).Fulfill(NullLogSink{})
				close(fulfillDone)
			}()
			go func() {
				defer GinkgoRecover()
				delegLog.WithName("with-name")
				close(withNameDone)
			}()
			go func() {
				defer GinkgoRecover()
				delegLog.WithValues("with-value")
				close(withValuesDone)
			}()
			go func() {
				defer GinkgoRecover()
				child.WithValues("grandchild")
				close(grandChildDone)
			}()
			go func() {
				defer GinkgoRecover()
				delegLog.Enabled()
				close(logEnabledDone)
			}()
			go func() {
				defer GinkgoRecover()
				delegLog.Info("hello world")
				close(logInfoDone)
			}()
			go func() {
				defer GinkgoRecover()
				delegLog.Error(errors.New("err"), "hello world")
				close(logErrorDone)
			}()
			go func() {
				defer GinkgoRecover()
				delegLog.V(1)
				close(logVDone)
			}()

			<-fulfillDone
			<-withNameDone
			<-withValuesDone
			<-grandChildDone
			<-logEnabledDone
			<-logInfoDone
			<-logErrorDone
			<-logVDone
		})

		It("should delegate with tags", func() {
			By("asking for a logSink with a name before fulfill, and logging")
			befFulfill1 := delegLog.WithValues("tag1", "val1")
			befFulfill2 := befFulfill1.WithValues("tag2", "val2")
			befFulfill1.Info("before fulfill")

			By("logging on the base logSink before fulfill")
			delegLog.Info("before fulfill base")

			By("ensuring that no messages were actually recorded")
			Expect(root.messages).To(BeEmpty())

			By("fulfilling the promise")
			delegLog.GetSink().(canFulfill).Fulfill(baseLog)

			By("logging with the existing loggers after fulfilling")
			befFulfill1.Info("after 1")
			befFulfill2.Info("after 2")

			By("grabbing a new sub-logSink of a previously constructed logSink and logging to it")
			befFulfill1.WithValues("tag3", "val3").Info("after 3")

			By("logging with new loggers")
			delegLog.WithValues("tag3", "val3").Info("after 4")

			By("ensuring that the messages are appropriately named")
			Expect(root.messages).To(ConsistOf(
				logInfo{tags: []interface{}{"tag1", "val1"}, msg: "after 1"},
				logInfo{tags: []interface{}{"tag1", "val1", "tag2", "val2"}, msg: "after 2"},
				logInfo{tags: []interface{}{"tag1", "val1", "tag3", "val3"}, msg: "after 3"},
				logInfo{tags: []interface{}{"tag3", "val3"}, msg: "after 4"},
			))
		})

		It("shouldn't fulfill twice", func() {
			By("fulfilling once")
			delegLog.GetSink().(canFulfill).Fulfill(baseLog)

			By("logging a bit")
			delegLog.Info("msg 1")

			By("fulfilling with a new logSink")
			delegLog.GetSink().(canFulfill).Fulfill(&fakeLogger{})

			By("logging some more")
			delegLog.Info("msg 2")

			By("checking that all log messages are present")
			Expect(root.messages).To(ConsistOf(
				logInfo{msg: "msg 1"},
				logInfo{msg: "msg 2"},
			))
		})
	})

	Describe("logSink from context", func() {
		It("should return default logSink when context is empty", func() {
			gotLog := FromContext(context.Background())
			Expect(gotLog).To(Not(BeNil()))
		})

		It("should return existing logSink", func() {
			root := &fakeLoggerRoot{}
			baseLog := &fakeLogger{root: root}

			wantLog := logr.New(baseLog).WithName("my-logSink")
			ctx := IntoContext(context.Background(), wantLog)

			gotLog := FromContext(ctx)
			Expect(gotLog).To(Not(BeNil()))

			gotLog.Info("test message")
			Expect(root.messages).To(ConsistOf(
				logInfo{name: []string{"my-logSink"}, msg: "test message"},
			))
		})

		It("should have added key-values", func() {
			root := &fakeLoggerRoot{}
			baseLog := &fakeLogger{root: root}

			wantLog := logr.New(baseLog).WithName("my-logSink")
			ctx := IntoContext(context.Background(), wantLog)

			gotLog := FromContext(ctx, "tag1", "value1")
			Expect(gotLog).To(Not(BeNil()))

			gotLog.Info("test message")
			Expect(root.messages).To(ConsistOf(
				logInfo{name: []string{"my-logSink"}, tags: []interface{}{"tag1", "value1"}, msg: "test message"},
			))
		})
	})

})
