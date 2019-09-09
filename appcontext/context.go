//
// Copyright (c) 2019 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package appcontext

import (
	syscontext "context"
	"errors"
	"time"

	"github.com/edgexfoundry/app-functions-sdk-go/internal/common"
	"github.com/edgexfoundry/app-functions-sdk-go/pkg/util"
	"github.com/edgexfoundry/go-mod-core-contracts/clients"
	"github.com/edgexfoundry/go-mod-core-contracts/clients/coredata"
	"github.com/edgexfoundry/go-mod-core-contracts/clients/logger"
	"github.com/edgexfoundry/go-mod-core-contracts/models"
	"github.com/google/uuid"
)

// AppFunction is a type alias for func(edgexcontext *appcontext.Context, params ...interface{}) (bool, interface{})
type AppFunction = func(edgexcontext *Context, params ...interface{}) (bool, interface{})

// Context ...
type Context struct {
	// ID of the EdgeX Event -- will be filled for a received JSON Event
	EventID string
	// Checksum of the EdgeX Event -- will be filled for a received CBOR Event
	EventChecksum string
	// This is the ID used to track the EdgeX event through entire EdgeX framework.
	CorrelationID string
	// OutputData is used for specifying the data that is to be outputted. Leverage the .Complete() function to set.
	OutputData []byte
	// This holds the configuration for your service. This is the preferred way to access your custom application settings that have been set in the configuration.
	Configuration common.ConfigurationStruct
	// This is exposed to allow logging following the preferred logging strategy within EdgeX.
	LoggingClient logger.LoggingClient
	EventClient   coredata.EventClient
}

// Complete is optional and provides a way to return the specified data.
// In the case of an HTTP Trigger, the data will be returned as the http response.
// In the case of the message bus trigger, the data will be placed on the specifed
// message bus publish topic and host in the configuration.
func (context *Context) Complete(output []byte) {
	context.OutputData = output
}

// MarkAsPushed will make a request to CoreData to mark the event that triggered the pipeline as pushed.
func (context *Context) MarkAsPushed() error {
	context.LoggingClient.Debug("Marking event as pushed")
	if context.EventID != "" {
		return context.EventClient.MarkPushed(context.EventID, syscontext.WithValue(syscontext.Background(), clients.CorrelationHeader, context.CorrelationID))
	} else if context.EventChecksum != "" {
		return context.EventClient.MarkPushedByChecksum(context.EventChecksum, syscontext.WithValue(syscontext.Background(), clients.CorrelationHeader, context.CorrelationID))
	} else {
		return errors.New("No EventID or EventChecksum Provided")
	}
}

// PushToCoreData pushes the provided value as an event to CoreData using the device name and reading name that have been set. If validation is turned on in
// CoreServices then your deviceName and readingName must exist in the CoreMetadata and be properly registered in EdgeX.
func (context *Context) PushToCoreData(deviceName string, readingName string, value interface{}) (*models.Event, error) {
	context.LoggingClient.Debug("Pushing to CoreData")
	now := time.Now().UnixNano()
	val, err := util.CoerceType(value)
	if err != nil {
		return nil, err
	}
	newReading := models.Reading{
		Value:  string(val),
		Origin: now,
		Device: deviceName,
		Name:   readingName,
	}

	readings := make([]models.Reading, 0, 1)
	readings = append(readings, newReading)

	newEdgeXEvent := &models.Event{
		Device:   deviceName,
		Origin:   now,
		Readings: readings,
	}

	correlation := uuid.New().String()
	ctx := syscontext.WithValue(syscontext.Background(), clients.CorrelationHeader, correlation)
	result, err := context.EventClient.Add(newEdgeXEvent, ctx)
	if err != nil {
		return nil, err
	}
	newEdgeXEvent.ID = result
	return newEdgeXEvent, nil
}