package client

import (
	"fmt"

	"github.com/sipeed/reef/pkg/reef"
)

// CNPHandler handles CNP messages on the client side.
type CNPHandler struct {
	taskID   string
	sendFunc func(msg reef.CNPMessage) error
}

// NewCNPHandler creates a client-side CNP handler.
func NewCNPHandler(taskID string, sendFunc func(reef.CNPMessage) error) *CNPHandler {
	return &CNPHandler{
		taskID:   taskID,
		sendFunc: sendFunc,
	}
}

func (h *CNPHandler) SendCorruption(corruptionType, tool string, count int, message string) error {
	msg := reef.NewCNPMessage(reef.MsgContextCorruption, h.taskID, reef.CorruptionPayload{
		Type:    corruptionType,
		Tool:    tool,
		Count:   count,
		Message: message,
	})
	return h.sendFunc(msg)
}

func (h *CNPHandler) SendMemoryUpdate(eventType, summary string, tags []string) error {
	msg := reef.NewCNPMessage(reef.MsgMemoryUpdate, h.taskID, reef.MemoryUpdatePayload{
		EventType: eventType,
		Summary:   summary,
		Tags:      tags,
	})
	return h.sendFunc(msg)
}

func (h *CNPHandler) SendCheckpoint(roundNum int, summary string) error {
	msg := reef.NewCNPMessage(reef.MsgCheckpointSave, h.taskID, reef.CheckpointSavePayload{
		RoundNum: roundNum,
		Summary:  summary,
	})
	return h.sendFunc(msg)
}

func (h *CNPHandler) SendCompactDone() error {
	msg := reef.NewCNPMessage(reef.MsgContextCompactDone, h.taskID, nil)
	return h.sendFunc(msg)
}

func (h *CNPHandler) SendStrategyAck(accepted bool, reason string) error {
	msg := reef.NewCNPMessage(reef.MsgStrategyAck, h.taskID, reef.StrategyAckPayload{
		Accepted: accepted,
		Reason:   reason,
	})
	return h.sendFunc(msg)
}

func (h *CNPHandler) SendStrategyResult(success bool, summary string) error {
	msg := reef.NewCNPMessage(reef.MsgStrategyResult, h.taskID, reef.StrategyResultPayload{
		Success: success,
		Summary: summary,
	})
	return h.sendFunc(msg)
}

func (h *CNPHandler) SendHeartbeat() error {
	msg := reef.NewCNPMessage(reef.MsgLongTaskHeartbeat, h.taskID, nil)
	return h.sendFunc(msg)
}

func (h *CNPHandler) SendProgress(roundNum int, progress float64, message string) error {
	msg := reef.NewCNPMessage(reef.MsgLongTaskProgress, h.taskID, reef.LongTaskProgressPayload{
		RoundNum: roundNum,
		Progress: progress,
		Message:  message,
	})
	return h.sendFunc(msg)
}

func (h *CNPHandler) SendComplete() error {
	msg := reef.NewCNPMessage(reef.MsgLongTaskComplete, h.taskID, nil)
	return h.sendFunc(msg)
}

func (h *CNPHandler) HandleServerMessage(msg reef.CNPMessage) error {
	switch msg.Type {
	case reef.MsgStrategySuggest:
		return nil // FUTURE
	case reef.MsgMemoryInject:
		return nil // FUTURE
	case reef.MsgContextInject:
		return nil // FUTURE
	case reef.MsgCheckpointRestore:
		return nil // FUTURE
	default:
		return fmt.Errorf("unhandled server message type: %s", msg.Type)
	}
}
