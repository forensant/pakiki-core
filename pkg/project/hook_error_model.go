package project

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// HookErrorLog is used to record errors which occur when running hooks
// in the future, my preference would be to run each hook individually within
// the Python interpreter so that we can record errors and still process
// the rest of the hooks
type HookErrorLog struct {
	ID           uint `json:"-"`
	GUID         string
	Time         time.Time
	Code         string
	Error        string
	Output       string
	ObjectType   string        `gorm:"-"`
	HookResponse *HookResponse `gorm:"-" json:"-"`
}

func (h *HookErrorLog) GetGUID() string {
	if h.GUID == "" {
		h.GUID = uuid.NewString()
	}

	return h.GUID
}

func (h *HookErrorLog) SetFullOutput(output string) {
	h.Output = output
}

func (h *HookErrorLog) SetOutput(output string) {
	h.Output += output
}

func (h *HookErrorLog) SetStatus(status string) {
	if status != "Running" {
		h.HookResponse.parseResponse(h)
	}
}

func (h *HookErrorLog) ShouldFilter(str string) bool {
	return false
}

func (h *HookErrorLog) Record() {
	if h.GUID == "" {
		h.GUID = uuid.NewString()
	}

	h.Time = time.Now()

	ioHub.databaseWriter <- h

	h.ObjectType = "Hook Error Log"
	ioHub.broadcast <- h
}

func (h *HookErrorLog) RecordError(err string) {
	h.Error = err
	h.Record()
	h.HookResponse.parseResponse(h)
}

func (h *HookErrorLog) WriteToDatabase(db *gorm.DB) {
	db.Save(h)
}
