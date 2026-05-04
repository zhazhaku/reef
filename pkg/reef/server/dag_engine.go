package server

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server/store"
)

// DAGEngine manages task decomposition, dependency tracking, and result aggregation.
type DAGEngine struct {
	mu        sync.Mutex
	Store     store.TaskStore
	scheduler *Scheduler
	logger    *slog.Logger
}

// NewDAGEngine creates a new DAG engine.
func NewDAGEngine(s store.TaskStore, scheduler *Scheduler, logger *slog.Logger) *DAGEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &DAGEngine{
		Store:     s,
		scheduler: scheduler,
		logger:    logger,
	}
}

// CreateSubTasks decomposes a parent task into sub-tasks and registers DAG relationships.
func (d *DAGEngine) CreateSubTasks(parentTask *reef.Task, plans []SubTaskPlan) ([]*reef.Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if parentTask == nil {
		return nil, fmt.Errorf("parent task is nil")
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("no sub-task plans provided")
	}

	subTasks := make([]*reef.Task, 0, len(plans))
	for _, plan := range plans {
		subID := fmt.Sprintf("%s-sub-%d", parentTask.ID, len(subTasks)+1)
		sub := reef.NewTask(subID, plan.Instruction, plan.Role, plan.Skills)
		sub.Priority = parentTask.Priority
		sub.ParentTaskID = parentTask.ID
		sub.Dependencies = plan.DependsOn
		sub.ModelHint = plan.ModelHint
		sub.TimeoutMs = plan.TimeoutMs
		if sub.TimeoutMs <= 0 {
			sub.TimeoutMs = parentTask.TimeoutMs
		}

		if plan.HasDependencies() {
			_ = sub.Transition(reef.TaskBlocked)
		}

		if err := d.Store.SaveTask(sub); err != nil {
			return nil, fmt.Errorf("save sub-task %s: %w", subID, err)
		}
		if err := d.Store.SaveRelation(parentTask.ID, subID, ""); err != nil {
			return nil, fmt.Errorf("save relation: %w", err)
		}

		subTasks = append(subTasks, sub)
	}

	parentTask.SubTaskIDs = make([]string, len(subTasks))
	for i, st := range subTasks {
		parentTask.SubTaskIDs[i] = st.ID
	}
	_ = d.Store.UpdateTask(parentTask)

	for _, st := range subTasks {
		if !st.Status.IsBlocked() {
			d.scheduler.RegisterTask(st)
			if err := d.scheduler.Submit(st); err != nil {
				d.logger.Warn("failed to submit sub-task",
					slog.String("task_id", st.ID),
					slog.String("error", err.Error()))
			}
		}
	}

	d.logger.Info("created sub-tasks",
		slog.String("parent_id", parentTask.ID),
		slog.Int("count", len(subTasks)),
	)
	return subTasks, nil
}

// SubTaskPlan describes a sub-task to be created by the DAG engine.
type SubTaskPlan struct {
	Instruction string
	Role        string
	Skills      []string
	ModelHint   string
	TimeoutMs   int64
	DependsOn   []string
}

func (p *SubTaskPlan) HasDependencies() bool {
	return len(p.DependsOn) > 0
}

// OnSubTaskCompleted handles completion of a sub-task and unblocks dependents.
func (d *DAGEngine) OnSubTaskCompleted(subTaskID string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	parentID, err := d.Store.GetParentTaskID(subTaskID)
	if err != nil {
		return false, fmt.Errorf("get parent: %w", err)
	}
	if parentID == "" {
		return false, nil
	}

	allDone, err := d.allSubTasksDone(parentID)
	if err != nil {
		return false, err
	}

	if allDone {
		parentTask := d.scheduler.GetTask(parentID)
		if parentTask != nil {
			_ = parentTask.Transition(reef.TaskAggregating)
			_ = d.Store.UpdateTask(parentTask)
		}
		return true, nil
	}

	unblocked, err := d.checkUnblock(parentID)
	if err != nil {
		return false, err
	}

	for _, ut := range unblocked {
		d.scheduler.RegisterTask(ut)
		if err := d.scheduler.Submit(ut); err != nil {
			d.logger.Warn("failed to submit unblocked sub-task",
				slog.String("task_id", ut.ID),
				slog.String("error", err.Error()))
		}
	}

	return false, nil
}

// OnSubTaskFailed handles failure of a sub-task and fails dependent tasks.
func (d *DAGEngine) OnSubTaskFailed(subTaskID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	parentID, err := d.Store.GetParentTaskID(subTaskID)
	if err != nil {
		return fmt.Errorf("get parent: %w", err)
	}
	if parentID == "" {
		return nil
	}

	siblings, err := d.Store.GetSubTaskIDs(parentID)
	if err != nil {
		return err
	}

	for _, sibID := range siblings {
		if sibID == subTaskID {
			continue
		}
		sib := d.scheduler.GetTask(sibID)
		if sib == nil {
			sib, err = d.Store.GetTask(sibID)
			if err != nil {
				continue
			}
		}
		for _, dep := range sib.Dependencies {
			if dep == subTaskID {
				sib.Error = &reef.TaskError{
					Type:    "dependency_failed",
					Message: fmt.Sprintf("dependency task %s failed", subTaskID),
				}
				_ = sib.Transition(reef.TaskFailed)
				_ = d.Store.UpdateTask(sib)
				break
			}
		}
	}

	return nil
}

func (d *DAGEngine) allSubTasksDone(parentID string) (bool, error) {
	subIDs, err := d.Store.GetSubTaskIDs(parentID)
	if err != nil {
		return false, err
	}
	if len(subIDs) == 0 {
		return false, nil
	}

	for _, sid := range subIDs {
		task := d.scheduler.GetTask(sid)
		if task == nil {
			task, err = d.Store.GetTask(sid)
			if err != nil {
				return false, err
			}
		}
		if !task.Status.IsTerminal() {
			return false, nil
		}
	}
	return true, nil
}

func (d *DAGEngine) checkUnblock(parentID string) ([]*reef.Task, error) {
	subIDs, err := d.Store.GetSubTaskIDs(parentID)
	if err != nil {
		return nil, err
	}

	var unblocked []*reef.Task
	for _, sid := range subIDs {
		task := d.scheduler.GetTask(sid)
		if task == nil {
			task, err = d.Store.GetTask(sid)
			if err != nil {
				continue
			}
		}
		if task.Status != reef.TaskBlocked {
			continue
		}

		depsMet := true
		for _, depID := range task.Dependencies {
			depTask := d.scheduler.GetTask(depID)
			if depTask == nil {
				depTask, err = d.Store.GetTask(depID)
				if err != nil {
					depsMet = false
					break
				}
			}
			if depTask.Status != reef.TaskCompleted {
				depsMet = false
				break
			}
		}

		if depsMet {
			_ = task.Transition(reef.TaskQueued)
			_ = d.Store.UpdateTask(task)
			unblocked = append(unblocked, task)
		}
	}

	return unblocked, nil
}

// BuildAggregationMessage collects sub-task results into a summary for the parent.
func (d *DAGEngine) BuildAggregationMessage(parentID string) (string, error) {
	subIDs, err := d.Store.GetSubTaskIDs(parentID)
	if err != nil {
		return "", err
	}

	results := make([]string, 0, len(subIDs))
	for _, sid := range subIDs {
		task := d.scheduler.GetTask(sid)
		if task == nil {
			task, err = d.Store.GetTask(sid)
			if err != nil {
				continue
			}
		}
		switch task.Status {
		case reef.TaskCompleted:
			if task.Result != nil {
				results = append(results, fmt.Sprintf("[%s]: %s", sid, task.Result.Text))
			}
		case reef.TaskFailed:
			if task.Error != nil {
				results = append(results, fmt.Sprintf("[%s] FAILED: %s", sid, task.Error.Message))
			}
		}
	}

	if len(results) == 0 {
		return "All sub-tasks completed with no output.", nil
	}

	msg := "Sub-task results:\n"
	for _, r := range results {
		msg += "- " + r + "\n"
	}
	return msg, nil
}
