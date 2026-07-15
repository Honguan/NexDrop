package transfer

import (
	"context"
	"errors"
	"strings"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/domain"
)

var (
	ErrInvalid       = errors.New("invalid transfer request")
	ErrNotFound      = errors.New("transfer not found")
	ErrForbidden     = errors.New("transfer operation forbidden")
	ErrFileTooLarge  = errors.New("file exceeds node limit")
	ErrQuotaExceeded = errors.New("storage or traffic quota exceeded")
	ErrStorageFull   = errors.New("node storage is full")
	ErrConflict      = errors.New("transfer state conflict")
)

type ContentType string

const (
	ContentText         ContentType = "TEXT"
	ContentURL          ContentType = "URL"
	ContentImage        ContentType = "IMAGE"
	ContentFile         ContentType = "FILE"
	ContentNotification ContentType = "NOTIFICATION"
)

type TargetType string

const (
	TargetSingle        TargetType = "SINGLE_DEVICE"
	TargetMultiple      TargetType = "MULTIPLE_DEVICES"
	TargetAllDevices    TargetType = "ALL_USER_DEVICES"
	TargetGroupAll      TargetType = "GROUP_ALL_DEVICES"
	TargetGroupSelected TargetType = "GROUP_SELECTED_DEVICES"
)

type File struct {
	ID         string    `json:"id,omitempty"`
	Name       string    `json:"name"`
	MIMEType   string    `json:"mimeType"`
	Size       int64     `json:"size"`
	SHA256     []byte    `json:"sha256"`
	ChunkSize  int       `json:"chunkSize"`
	ChunkCount int       `json:"chunkCount"`
	ExpiresAt  time.Time `json:"expiresAt,omitempty"`
}

type Request struct {
	ClientBatchID         string            `json:"clientBatchId,omitempty"`
	TargetType            TargetType        `json:"targetType"`
	TargetDeviceIDs       []string          `json:"targetDeviceIds"`
	GroupID               string            `json:"groupId,omitempty"`
	LANAvailableDeviceIDs []string          `json:"lanAvailableDeviceIds,omitempty"`
	ContentType           ContentType       `json:"contentType"`
	Content               []byte            `json:"content,omitempty"`
	Files                 []File            `json:"files,omitempty"`
	RouteMode             domain.RouteMode  `json:"routeMode"`
	AllowLargeFileViaNode bool              `json:"allowLargeFileViaNode"`
	WrappedContentKeys    map[string][]byte `json:"wrappedContentKeys,omitempty"`
}

type Target struct {
	DeviceID         string                `json:"deviceId"`
	SelectedRoute    domain.SelectedRoute  `json:"selectedRoute"`
	Status           domain.TransferStatus `json:"status"`
	BytesTransferred int64                 `json:"bytesTransferred"`
}

type FileTarget struct {
	FileIndex     int                   `json:"fileIndex"`
	DeviceID      string                `json:"deviceId"`
	SelectedRoute domain.SelectedRoute  `json:"selectedRoute"`
	Status        domain.TransferStatus `json:"status"`
}

type Prepared struct {
	Request
	ResolvedDeviceIDs []string
	Targets           []Target
	FileTargets       []FileTarget
	Status            domain.TransferStatus
	CreatedAt         time.Time
	ExpiresAt         time.Time
}

type Transfer struct {
	ID                 string                `json:"id"`
	BatchID            string                `json:"batchId,omitempty"`
	SenderUserID       string                `json:"senderUserId"`
	SenderDeviceID     string                `json:"senderDeviceId,omitempty"`
	TargetType         TargetType            `json:"targetType"`
	GroupID            string                `json:"groupId,omitempty"`
	ContentType        ContentType           `json:"contentType"`
	Content            []byte                `json:"content,omitempty"`
	Files              []File                `json:"files"`
	Targets            []Target              `json:"targets"`
	FileTargets        []FileTarget          `json:"fileTargets,omitempty"`
	WrappedContentKeys map[string][]byte     `json:"wrappedContentKeys,omitempty"`
	Status             domain.TransferStatus `json:"status"`
	CreatedAt          time.Time             `json:"createdAt"`
	ExpiresAt          time.Time             `json:"expiresAt"`
}

type Progress struct {
	DeviceID         string                `json:"deviceId"`
	Status           domain.TransferStatus `json:"status"`
	Route            domain.SelectedRoute  `json:"route,omitempty"`
	BytesTransferred int64                 `json:"bytesTransferred"`
	ErrorCode        string                `json:"errorCode,omitempty"`
}

type Store interface {
	ResolveTransferTargets(context.Context, auth.Session, TargetType, string, []string) ([]string, error)
	CreateTransfer(context.Context, auth.Session, Prepared) (Transfer, error)
	ListTransfers(context.Context, auth.Session) ([]Transfer, error)
	GetTransfer(context.Context, auth.Session, string) (Transfer, error)
	CancelTransfer(context.Context, auth.Session, string, time.Time) (Transfer, error)
	HideTransfer(context.Context, auth.Session, string, time.Time) error
	ReadTransfer(context.Context, auth.Session, string, time.Time) (Transfer, error)
	ReportTransferProgress(context.Context, auth.Session, string, Progress, time.Time) (Transfer, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (service *Service) Create(ctx context.Context, session auth.Session, request Request) (Transfer, error) {
	if session.DeviceID == nil {
		return Transfer{}, ErrForbidden
	}
	if err := validateRequest(request); err != nil {
		return Transfer{}, err
	}
	resolved, err := service.store.ResolveTransferTargets(ctx, session, request.TargetType, request.GroupID, request.TargetDeviceIDs)
	if err != nil {
		return Transfer{}, err
	}
	if len(resolved) == 0 {
		return Transfer{}, ErrInvalid
	}
	for _, deviceID := range resolved {
		if len(request.WrappedContentKeys[deviceID]) == 0 {
			return Transfer{}, ErrInvalid
		}
	}
	lanAvailable := make(map[string]bool, len(request.LANAvailableDeviceIDs))
	for _, id := range request.LANAvailableDeviceIDs {
		lanAvailable[id] = true
	}
	prepared := Prepared{
		Request:           request,
		ResolvedDeviceIDs: resolved,
		CreatedAt:         service.now().UTC(),
	}
	if isTextContent(request.ContentType) {
		prepared.ExpiresAt = prepared.CreatedAt.Add(90 * 24 * time.Hour)
		for _, deviceID := range resolved {
			route := domain.SelectRoute(domain.RouteRequest{Mode: request.RouteMode, LANAvailable: lanAvailable[deviceID], NodeAvailable: true, TextContent: true})
			prepared.Targets = append(prepared.Targets, Target{DeviceID: deviceID, SelectedRoute: route, Status: initialStatus(route)})
		}
	} else {
		prepared.ExpiresAt = prepared.CreatedAt.Add(7 * 24 * time.Hour)
		for _, deviceID := range resolved {
			routes := make(map[domain.SelectedRoute]bool)
			for fileIndex, file := range request.Files {
				route := domain.SelectRoute(domain.RouteRequest{
					Mode: request.RouteMode, LANAvailable: lanAvailable[deviceID], NodeAvailable: true,
					FileSize: file.Size, AllowLargeFileViaNode: request.AllowLargeFileViaNode,
				})
				routes[route] = true
				prepared.FileTargets = append(prepared.FileTargets, FileTarget{FileIndex: fileIndex, DeviceID: deviceID, SelectedRoute: route, Status: initialStatus(route)})
			}
			route := onlyRoute(routes)
			prepared.Targets = append(prepared.Targets, Target{DeviceID: deviceID, SelectedRoute: route, Status: initialStatus(route)})
		}
	}
	prepared.Status = aggregateStatus(prepared.Targets)
	return service.store.CreateTransfer(ctx, session, prepared)
}

func (service *Service) List(ctx context.Context, session auth.Session) ([]Transfer, error) {
	return service.store.ListTransfers(ctx, session)
}

func (service *Service) Get(ctx context.Context, session auth.Session, id string) (Transfer, error) {
	if id == "" {
		return Transfer{}, ErrInvalid
	}
	return service.store.GetTransfer(ctx, session, id)
}

func (service *Service) Cancel(ctx context.Context, session auth.Session, id string) (Transfer, error) {
	if id == "" {
		return Transfer{}, ErrInvalid
	}
	return service.store.CancelTransfer(ctx, session, id, service.now().UTC())
}

func (service *Service) Hide(ctx context.Context, session auth.Session, id string) error {
	if id == "" {
		return ErrInvalid
	}
	return service.store.HideTransfer(ctx, session, id, service.now().UTC())
}

func (service *Service) Read(ctx context.Context, session auth.Session, id string) (Transfer, error) {
	if id == "" || session.DeviceID == nil {
		return Transfer{}, ErrForbidden
	}
	return service.store.ReadTransfer(ctx, session, id, service.now().UTC())
}

func (service *Service) ReportProgress(ctx context.Context, session auth.Session, id string, progress Progress) (Transfer, error) {
	if id == "" || session.DeviceID == nil || progress.DeviceID == "" || progress.BytesTransferred < 0 || !reportableStatus(progress.Status) || !reportableRoute(progress.Route) || len(progress.ErrorCode) > 100 {
		return Transfer{}, ErrInvalid
	}
	for _, character := range progress.ErrorCode {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '_' {
			return Transfer{}, ErrInvalid
		}
	}
	return service.store.ReportTransferProgress(ctx, session, id, progress, service.now().UTC())
}

func validateRequest(request Request) error {
	if !validTargetType(request.TargetType) || !validContentType(request.ContentType) {
		return ErrInvalid
	}
	if hasDuplicates(request.TargetDeviceIDs) {
		return ErrInvalid
	}
	if (request.TargetType == TargetGroupAll || request.TargetType == TargetGroupSelected) != (request.GroupID != "") {
		return ErrInvalid
	}
	if request.TargetType == TargetSingle && len(request.TargetDeviceIDs) != 1 {
		return ErrInvalid
	}
	if (request.TargetType == TargetMultiple || request.TargetType == TargetGroupSelected) && len(request.TargetDeviceIDs) == 0 {
		return ErrInvalid
	}
	if isTextContent(request.ContentType) {
		if len(request.Content) == 0 || len(request.Files) != 0 {
			return ErrInvalid
		}
	} else {
		if len(request.Files) == 0 {
			return ErrInvalid
		}
		for _, file := range request.Files {
			if strings.TrimSpace(file.Name) == "" || file.Size < 0 || len(file.SHA256) != 32 || file.ChunkSize <= 0 {
				return ErrInvalid
			}
			expectedChunkCount := int(file.Size / int64(file.ChunkSize))
			if file.Size%int64(file.ChunkSize) != 0 {
				expectedChunkCount++
			}
			if file.ChunkCount != expectedChunkCount {
				return ErrInvalid
			}
		}
	}
	return nil
}

func isTextContent(value ContentType) bool {
	return value == ContentText || value == ContentURL || value == ContentNotification
}
func validContentType(value ContentType) bool {
	return isTextContent(value) || value == ContentImage || value == ContentFile
}
func validTargetType(value TargetType) bool {
	return value == TargetSingle || value == TargetMultiple || value == TargetAllDevices || value == TargetGroupAll || value == TargetGroupSelected
}

func reportableStatus(value domain.TransferStatus) bool {
	switch value {
	case domain.TransferCheckingRoute, domain.TransferWaitingForTarget, domain.TransferWaitingForNode,
		domain.TransferWaitingForLAN, domain.TransferQueued, domain.TransferUploadingToNode,
		domain.TransferAvailableOnNode, domain.TransferDownloading, domain.TransferTransferringLAN,
		domain.TransferPaused, domain.TransferVerifying, domain.TransferDelivered, domain.TransferRead,
		domain.TransferFailed, domain.TransferSourceFileMissing, domain.TransferSourceFileChanged:
		return true
	default:
		return false
	}
}

func reportableRoute(value domain.SelectedRoute) bool {
	switch value {
	case "", domain.SelectedRouteLAN, domain.SelectedRouteNode, domain.SelectedRouteWaitingLAN, domain.SelectedRouteDraft:
		return true
	default:
		return false
	}
}

func hasDuplicates(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func initialStatus(route domain.SelectedRoute) domain.TransferStatus {
	switch route {
	case domain.SelectedRouteWaitingLAN:
		return domain.TransferWaitingForLAN
	case domain.SelectedRouteNone:
		return domain.TransferFailed
	default:
		return domain.TransferQueued
	}
}

func onlyRoute(routes map[domain.SelectedRoute]bool) domain.SelectedRoute {
	if len(routes) != 1 {
		return domain.SelectedRouteMixed
	}
	for route := range routes {
		return route
	}
	return domain.SelectedRouteNone
}

func aggregateStatus(targets []Target) domain.TransferStatus {
	allWaiting := true
	for _, target := range targets {
		if target.Status != domain.TransferWaitingForLAN {
			allWaiting = false
			break
		}
	}
	if allWaiting {
		return domain.TransferWaitingForLAN
	}
	return domain.TransferQueued
}
