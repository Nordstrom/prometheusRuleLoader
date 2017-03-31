# Message batching queue
### Docs ###
https://godoc.org/gopkg.in/cheggaaa/mb.v1
### Installation ###
```go get -u gopkg.in/cheggaaa/mb.v1```

### Example ###
```Go
package main

import (
	"fmt"
	"time"

	"gopkg.in/cheggaaa/mb.v1"
)

func main() {
	// create the queue with 10 items capacity
	q := mb.New(10)

	// create the channel for showing when all work will be done
	done := make(chan bool)

	// start two workers
	go worker("first", q, done)
	go worker("second", q, done)

	// start two publishers
	go publisher("first", q)
	go publisher("second", q)

	// give time to work
	time.Sleep(time.Second)

	// close the queue
	q.Close()

	// and wait until all sent messages will be processed
	for i := 0; i < 2; i++ {
		<-done
	}
}

func publisher(name string, q *mb.MB) {
	fmt.Printf("Publisher %s: started\n", name)
	var i int
	for {
		// will sending name and counter
		msg := fmt.Sprintf("%s - %d", name, i)
		// add
		if err := q.Add(msg); err != nil {
			// non-nil err mean that queue is closed
			break
		}
		// 10 messages per second
		time.Sleep(time.Second / 10)
		i++
	}
	fmt.Printf("Publisher %s: closed\n", name)
}

func worker(name string, q *mb.MB, done chan bool) {
	fmt.Printf("Worker %s: started\n", name)
	for {
		// getting messages
		msgs := q.Wait()

		if len(msgs) == 0 {
			// 0 messages mean that queue is closed
			break
		}

		msgsForPrint := ""
		for _, msg := range msgs {
			msgsForPrint += fmt.Sprintf("\t%s\n", msg)
		}
		fmt.Printf("Worker %s: %d messages recieved\n%s", name, len(msgs), msgsForPrint)

		// doing working, for example, send messages to remote server
		time.Sleep(time.Second / 3)
	}
	fmt.Printf("Worker %s: closed\n", name)
	done <- true
}

```

