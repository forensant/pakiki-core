package request_queue

import (
	"sync"
)

var requestQueueMutex sync.Mutex
var requestQueueCount map[string]int
var requestQueueChannels map[string](chan bool)

func Init() {
	requestQueueMutex.Lock()
	requestQueueCount = make(map[string]int)
	requestQueueChannels = make(map[string](chan bool))
	requestQueueMutex.Unlock()
}

func Close() {
	requestQueueMutex.Lock()
	requestQueueCount = make(map[string]int)
	for guid := range requestQueueChannels {
		close(requestQueueChannels[guid])
	}
	requestQueueChannels = make(map[string](chan bool))
	requestQueueMutex.Unlock()
}

func CloseQueueIfEmpty(guid string) {
	requestQueueMutex.Lock()

	if requestQueueCount[guid] <= 0 {
		delete(requestQueueCount, guid)
		if channel, channelExists := requestQueueChannels[guid]; channelExists {
			close(channel)
			delete(requestQueueChannels, guid)
		}
	}

	requestQueueMutex.Unlock()
}

func CancelRequests(guid string) {
	requestQueueMutex.Lock()

	delete(requestQueueCount, guid)
	close(requestQueueChannels[guid])
	delete(requestQueueChannels, guid)

	requestQueueMutex.Unlock()
}

func Channel(guid string) chan bool {
	requestQueueMutex.Lock()
	if _, existing := requestQueueChannels[guid]; !existing {
		requestQueueChannels[guid] = make(chan bool)
	}

	c := requestQueueChannels[guid]
	requestQueueMutex.Unlock()
	return c
}

func Decrement(guid string) {
	requestQueueMutex.Lock()
	if _, existing := requestQueueCount[guid]; existing {
		requestQueueCount[guid] -= 1
	}

	requestQueueMutex.Unlock()

	CloseQueueIfEmpty(guid)
}

func Increment(guid string) {
	requestQueueMutex.Lock()
	if _, existing := requestQueueCount[guid]; !existing {
		requestQueueCount[guid] = 0
	}

	requestQueueCount[guid] += 1
	requestQueueMutex.Unlock()
}
