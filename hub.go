package broadcaster

import "sync"

type hub struct {
	Running bool

	startlock sync.Mutex
}

func (h *hub) Start() {
	h.startlock.Lock()
	if !h.Running {
		h.Running = true
		go h.run()
	}
	h.startlock.Unlock()
}

func (h *hub) run() {
}
