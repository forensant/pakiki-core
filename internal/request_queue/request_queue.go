package request_queue

import (
	"sync"
)

type QueueableOperation interface {
	GetGUID() string
	IncrementRequestCount()
	Broadcast()
}

var requestQueueMutex sync.Mutex
var requestQueueCount map[string]int
var requestQueueInjectOperations map[string]QueueableOperation
var requestQueueChannels map[string](chan bool)

func Init() {
	requestQueueMutex.Lock()
	requestQueueCount = make(map[string]int)
	requestQueueChannels = make(map[string](chan bool))
	requestQueueInjectOperations = make(map[string]QueueableOperation)
	requestQueueMutex.Unlock()
}

func Add(op QueueableOperation) {
	requestQueueMutex.Lock()
	requestQueueInjectOperations[op.GetGUID()] = op
	requestQueueMutex.Unlock()
}

func CancelRequests(guid string) {
	requestQueueMutex.Lock()

	delete(requestQueueCount, guid)
	delete(requestQueueInjectOperations, guid)
	if _, exists := requestQueueChannels[guid]; exists {
		close(requestQueueChannels[guid])
	}
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

func Contains(guid string) bool {
	exists := false
	requestQueueMutex.Lock()
	if _, existing := requestQueueCount[guid]; existing {
		exists = true
	}

	if _, existing := requestQueueInjectOperations[guid]; existing {
		exists = true
	}
	requestQueueMutex.Unlock()

	return exists
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

func Decrement(guid string) {
	requestQueueMutex.Lock()
	if _, existing := requestQueueCount[guid]; existing {
		requestQueueCount[guid] -= 1
	}

	if _, existing := requestQueueInjectOperations[guid]; existing {
		requestQueueInjectOperations[guid].IncrementRequestCount()
		requestQueueInjectOperations[guid].Broadcast()
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

func IncrementBy(guid string, amount int) {
	requestQueueMutex.Lock()
	if _, existing := requestQueueCount[guid]; !existing {
		requestQueueCount[guid] = 0
	}

	requestQueueCount[guid] += amount
	requestQueueMutex.Unlock()
}
