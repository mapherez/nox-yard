package inventory

import "sync"

type Change struct {
	Inventory bool `json:"inventory"`
	Metrics   bool `json:"metrics"`
}

// Notifier coalesces invalidations without blocking Docker or dropping a kind
// of update when a browser is slow. Clients reconcile current state on connect.
type Notifier struct {
	mu          sync.Mutex
	subscribers map[chan Change]struct{}
	operations  map[string]map[uint64]string
	sequence    uint64
}

func NewNotifier() *Notifier {
	return &Notifier{subscribers: make(map[chan Change]struct{}), operations: make(map[string]map[uint64]string)}
}

func (n *Notifier) Notify(change Change) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subscribers {
		pending := change
		select {
		case prior := <-ch:
			pending.Inventory = pending.Inventory || prior.Inventory
			pending.Metrics = pending.Metrics || prior.Metrics
		default:
		}
		ch <- pending
	}
}

func (n *Notifier) Subscribe() (<-chan Change, func()) {
	ch := make(chan Change, 1)
	n.mu.Lock()
	n.subscribers[ch] = struct{}{}
	n.mu.Unlock()
	return ch, func() { n.mu.Lock(); delete(n.subscribers, ch); n.mu.Unlock() }
}

func OperationState(operation string) string {
	switch operation {
	case "start", "new", "copy":
		return "starting"
	case "stop":
		return "stopping"
	case "restart":
		return "restarting"
	case "remove":
		return "removing"
	case "update", "sync":
		return "updating"
	default:
		return ""
	}
}

func (n *Notifier) Begin(target, operation string) func() {
	n.mu.Lock()
	n.sequence++
	id := n.sequence
	if n.operations[target] == nil {
		n.operations[target] = make(map[uint64]string)
	}
	n.operations[target][id] = OperationState(operation)
	n.mu.Unlock()
	n.Notify(Change{Inventory: true})
	return func() {
		n.mu.Lock()
		delete(n.operations[target], id)
		if len(n.operations[target]) == 0 {
			delete(n.operations, target)
		}
		n.mu.Unlock()
		n.Notify(Change{Inventory: true})
	}
}

func (n *Notifier) Apply(snapshot *Snapshot) {
	n.mu.Lock()
	defer n.mu.Unlock()
	state := func(target string) string {
		var first uint64
		var operation string
		for id, value := range n.operations[target] {
			if first == 0 || id < first {
				first, operation = id, value
			}
		}
		return operation
	}
	for i := range snapshot.Projects {
		project := &snapshot.Projects[i]
		project.Operation = state(project.ID)
		for j := range project.Containers {
			item := &project.Containers[j]
			item.Operation = state("container:" + item.ID)
			if item.Operation == "" {
				item.Operation = project.Operation
			}
		}
		if project.Operation == "" {
			for _, item := range project.Containers {
				if item.Operation != "" {
					project.Operation = item.Operation
					break
				}
			}
		}
	}
}
