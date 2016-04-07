package main

import "sync"

/*
 Simple interface that allows us to switch out both implementations of the Manager
*/
type ConfigManager interface {
	Set(*string)
	Get() *string
	Close()
}

/*
 This struct manages the configuration instance by
 preforming locking around access to the Config struct.
*/
type MutexConfigManager struct {
	conf  string
	mutex *sync.Mutex
}

func NewMutexConfigManager(conf string) *MutexConfigManager {
	return &MutexConfigManager{conf, &sync.Mutex{}}
}

func (self *MutexConfigManager) Set(conf string) {
	self.mutex.Lock()
	self.conf = conf
	self.mutex.Unlock()
}

func (self *MutexConfigManager) Get() string {
	self.mutex.Lock()
	temp := self.conf
	self.mutex.Unlock()
	return temp
}

func (self *MutexConfigManager) Close() {
	//Do Nothing
}

/*
 This struct manages the configuration instance by feeding a
 pointer through a channel whenever the user calls Get()
*/
type ChannelConfigManager struct {
	conf string
	get  chan string
	set  chan string
	done chan bool
}

func NewChannelConfigManager(conf string) *ChannelConfigManager {
	parser := &ChannelConfigManager{conf, make(chan string), make(chan string), make(chan bool)}
	parser.Start()
	return parser
}

func (self *ChannelConfigManager) Start() {
	go func() {
		defer func() {
			close(self.get)
			close(self.set)
			close(self.done)
		}()
		for {
			select {
			case self.get <- self.conf:
			case value := <-self.set:
				self.conf = value
			case <-self.done:
				return
			}
		}
	}()
}

func (self *ChannelConfigManager) Close() {
	self.done <- true
}

func (self *ChannelConfigManager) Set(conf string) {
	self.set <- conf
}

func (self *ChannelConfigManager) Get() string {
	return <-self.get
}
