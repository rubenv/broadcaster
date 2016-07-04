package broadcaster

import (
	"time"

	"github.com/eapache/go-resiliency/retrier"
)

func newConnectionRetrier(class retrier.Classifier) *retrier.Retrier {
	waitIntervals := limitWait(retrier.ExponentialBackoff(100, 500*time.Millisecond), 10*time.Second)
	return retrier.New(waitIntervals, class)
}

func limitWait(dur []time.Duration, max time.Duration) []time.Duration {
	for i, w := range dur {
		if w > max {
			dur[i] = max
		}
	}
	return dur
}
