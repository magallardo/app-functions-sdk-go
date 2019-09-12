/*******************************************************************************
 * Copyright 2019 Dell Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the License
 * is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express
 * or implied. See the License for the specific language governing permissions and limitations under
 * the License.
 *******************************************************************************/

// mongo provides the Mongo implementation of the StoreClient interface.
package mongo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/edgexfoundry/app-functions-sdk-go/internal/store/contracts"
	"github.com/edgexfoundry/app-functions-sdk-go/internal/store/db"
	"github.com/edgexfoundry/app-functions-sdk-go/internal/store/db/interfaces"
	"github.com/edgexfoundry/app-functions-sdk-go/internal/store/db/mongo/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Client provides a wrapper for Mongo's Client type
type Client struct {
	Timeout time.Duration
	Client  *mongo.Database
	// unexported Client for Disconnect
	client *mongo.Client
}

const mongoCollection = "store"

// Store persists a stored object to the data store.
func (c Client) Store(o contracts.StoredObject) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	objID, uuid, err := models.FromContractId(o.ID)
	if err != nil {
		return "", err
	}
	var doc bson.M

	if objID == primitive.NilObjectID {
		// determine if this object already exists in the DB
		filter := bson.M{"uuid": uuid}
		result := c.Client.Collection(mongoCollection).FindOne(ctx, filter)

		var m models.StoredObject
		_ = result.Decode(&m)

		// if the result of the lookup is any object other than the empty, it exists
		if !reflect.DeepEqual(m, models.StoredObject{}) {
			return "", errors.New("object exists in database")
		}

		doc = bson.M{
			"uuid":             uuid,
			"appServiceKey":    o.AppServiceKey,
			"payload":          o.Payload,
			"retryCount":       o.RetryCount,
			"pipelinePosition": o.PipelinePosition,
			"version":          o.Version,
			"correlationID":    o.CorrelationID,
			"eventID":          o.EventID,
			"eventChecksum":    o.EventChecksum,
		}
	} else {
		doc = bson.M{
			"_id":              objID,
			"uuid":             uuid,
			"appServiceKey":    o.AppServiceKey,
			"payload":          o.Payload,
			"retryCount":       o.RetryCount,
			"pipelinePosition": o.PipelinePosition,
			"version":          o.Version,
			"correlationID":    o.CorrelationID,
			"eventID":          o.EventID,
			"eventChecksum":    o.EventChecksum,
		}
	}

	result, err := c.Client.Collection(mongoCollection).InsertOne(ctx, doc)
	if err != nil {
		return "", err
	}

	return result.InsertedID.(primitive.ObjectID).Hex(), nil
}

// RetrieveFromStore gets an object from the data store.
func (c Client) RetrieveFromStore(appServiceKey string) (objects []contracts.StoredObject, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	filter := bson.M{"appServiceKey": appServiceKey}

	// find all documents
	cursor, err := c.Client.Collection(mongoCollection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}

	var modelSlice []models.StoredObject

	// iterate through all documents
	for cursor.Next(ctx) {
		var p models.StoredObject
		// decode the document
		if err = cursor.Decode(&p); err != nil {
			return nil, err
		}
		modelSlice = append(modelSlice, p)
	}

	// check if the cursor encountered any errors while iterating
	if err = cursor.Err(); err != nil {
		return nil, err
	}

	for _, model := range modelSlice {
		objects = append(objects, model.ToContract())
	}

	return objects, nil
}

// Update replaces the data currently in the store with the provided data.
func (c Client) Update(o contracts.StoredObject) error {
	if o.ID == "" {
		return errors.New("update argument object does not have an UUID")
	}

	model := models.StoredObject{}
	err := model.FromContract(o)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	filter := bson.M{"_id": model.ObjectID}

	_, err = c.Client.Collection(mongoCollection).ReplaceOne(ctx, filter, &model)
	if err != nil {
		return err
	}

	return nil
}

// UpdateRetryCount modifies the RetryCount variable for a given object.
func (c Client) UpdateRetryCount(id string, count int) error {
	if id == "" {
		return errors.New("no UUID provided")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	objID, _, err := models.FromContractId(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objID}
	update := bson.M{"$set": bson.M{"retryCount": count}}

	_, err = c.Client.Collection(mongoCollection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	return nil
}

// RemoveFromStore removes an object from the data store.
func (c Client) RemoveFromStore(id string) error {
	if id == "" {
		return errors.New("no UUID provided")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	objID, _, err := models.FromContractId(id)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": objID}

	_, err = c.Client.Collection(mongoCollection).DeleteOne(ctx, filter)
	if err != nil {
		return err
	}

	return nil
}

func (c Client) Disconnect() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	return c.client.Disconnect(ctx)
}

// NewClient provides a factory for building a StoreClient
func NewClient(config db.Configuration) (client interfaces.StoreClient, err error) {
	var uri string
	if config.Username == "" && config.Password == "" {
		// no auth path
		uri = fmt.Sprintf("mongodb://%s:%s", config.Host, strconv.Itoa(config.Port))
	} else {
		uri = fmt.Sprintf("mongodb://%s:%s@%s:%s", config.Username, config.Password, config.Host, strconv.Itoa(config.Port))
	}

	timeout := time.Duration(config.Timeout) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	notify := make(chan bool)

	var mongoDatabase *mongo.Database
	var mongoClient *mongo.Client
	go func() {
		mongoClient, err = mongo.Connect(ctx, options.Client().ApplyURI(uri))
		if err != nil {
			cancel()
			return
		}

		// check the connection
		err = mongoClient.Ping(ctx, nil)
		if err != nil {
			cancel()
			return
		}

		mongoDatabase = mongoClient.Database(config.DatabaseName)

		// ping the watcher and tell it we're done
		notify <- true
	}()

	select {
	case <-ctx.Done():
		if err != nil {
			// an error from the business logic
			return nil, err
		} else {
			// timeout exceeded
			return nil, ctx.Err()
		}
	case <-notify:
		return Client{timeout, mongoDatabase, mongoClient}, nil
	}
}
