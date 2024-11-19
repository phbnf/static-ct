// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/transparency-dev/static-ct (interfaces: Storage)

// Package mockstorage is a generated GoMock package.
package mockstorage

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	x509 "github.com/google/certificate-transparency-go/x509"
	dedup "github.com/transparency-dev/static-ct/modules/dedup"
	tessera "github.com/transparency-dev/trillian-tessera"
	ctonly "github.com/transparency-dev/trillian-tessera/ctonly"
)

// MockStorage is a mock of Storage interface.
type MockStorage struct {
	ctrl     *gomock.Controller
	recorder *MockStorageMockRecorder
}

// MockStorageMockRecorder is the mock recorder for MockStorage.
type MockStorageMockRecorder struct {
	mock *MockStorage
}

// NewMockStorage creates a new mock instance.
func NewMockStorage(ctrl *gomock.Controller) *MockStorage {
	mock := &MockStorage{ctrl: ctrl}
	mock.recorder = &MockStorageMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockStorage) EXPECT() *MockStorageMockRecorder {
	return m.recorder
}

// Add mocks base method.
func (m *MockStorage) Add(arg0 context.Context, arg1 *ctonly.Entry) tessera.IndexFuture {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Add", arg0, arg1)
	ret0, _ := ret[0].(tessera.IndexFuture)
	return ret0
}

// Add indicates an expected call of Add.
func (mr *MockStorageMockRecorder) Add(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Add", reflect.TypeOf((*MockStorage)(nil).Add), arg0, arg1)
}

// AddCertDedupInfo mocks base method.
func (m *MockStorage) AddCertDedupInfo(arg0 context.Context, arg1 *x509.Certificate, arg2 dedup.SCTDedupInfo) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddCertDedupInfo", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddCertDedupInfo indicates an expected call of AddCertDedupInfo.
func (mr *MockStorageMockRecorder) AddCertDedupInfo(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddCertDedupInfo", reflect.TypeOf((*MockStorage)(nil).AddCertDedupInfo), arg0, arg1, arg2)
}

// AddIssuerChain mocks base method.
func (m *MockStorage) AddIssuerChain(arg0 context.Context, arg1 []*x509.Certificate) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddIssuerChain", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddIssuerChain indicates an expected call of AddIssuerChain.
func (mr *MockStorageMockRecorder) AddIssuerChain(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddIssuerChain", reflect.TypeOf((*MockStorage)(nil).AddIssuerChain), arg0, arg1)
}

// GetCertDedupInfo mocks base method.
func (m *MockStorage) GetCertDedupInfo(arg0 context.Context, arg1 *x509.Certificate) (dedup.SCTDedupInfo, bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCertDedupInfo", arg0, arg1)
	ret0, _ := ret[0].(dedup.SCTDedupInfo)
	ret1, _ := ret[1].(bool)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetCertDedupInfo indicates an expected call of GetCertDedupInfo.
func (mr *MockStorageMockRecorder) GetCertDedupInfo(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCertDedupInfo", reflect.TypeOf((*MockStorage)(nil).GetCertDedupInfo), arg0, arg1)
}
