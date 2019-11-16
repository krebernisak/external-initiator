package store

import (
	"bytes"
	"database/sql/driver"
	"encoding/csv"
	"fmt"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/pkg/errors"
	"github.com/smartcontractkit/external-initiator/store/migrations"
	"log"
)

const sqlDialect = "postgres"

// SQLStringArray is a string array stored in the database as comma separated values.
type SQLStringArray []string

// Scan implements the sql Scanner interface.
func (arr *SQLStringArray) Scan(src interface{}) error {
	if src == nil {
		*arr = nil
	}
	v, err := driver.String.ConvertValue(src)
	if err != nil {
		return errors.New("failed to scan StringArray")
	}
	str, ok := v.(string)
	if !ok {
		return nil
	}

	buf := bytes.NewBufferString(str)
	r := csv.NewReader(buf)
	ret, err := r.Read()
	if err != nil {
		return errors.Wrap(err, "badly formatted csv string array")
	}
	*arr = ret
	return nil
}

// Value implements the driver Valuer interface.
func (arr SQLStringArray) Value() (driver.Value, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	err := w.Write(arr)
	if err != nil {
		return nil, errors.Wrap(err, "csv encoding of string array")
	}
	w.Flush()
	return buf.String(), nil
}

type Client struct {
	db *gorm.DB
}

func ConnectToDb(uri string) (*Client, error) {
	db, err := gorm.Open(sqlDialect, uri)
	if err != nil {
		return nil, fmt.Errorf("unable to open %s for gorm DB: %+v", uri, err)
	}
	if err = migrations.Migrate(db); err != nil {
		return nil, errors.Wrap(err, "newDBStore#Migrate")
	}
	store := &Client{
		db: db.Set("gorm:auto_preload", true),
	}
	return store, nil
}

func (client Client) Close() error {
	return client.db.Close()
}

func (client Client) LoadSubscriptions() ([]Subscription, error) {
	var sqlSubs []*Subscription
	if err := client.db.Find(&sqlSubs).Error; err != nil {
		return nil, err
	}

	var subs []Subscription
	for _, sqlSub := range sqlSubs {
		endpoint, err := client.LoadEndpoint(sqlSub.EndpointName)
		if err != nil {
			log.Println(err)
			continue
		}

		sub := Subscription{
			Model:        sqlSub.Model,
			ReferenceId:  sqlSub.ReferenceId,
			Job:          sqlSub.Job,
			EndpointName: sqlSub.EndpointName,
			Endpoint:     endpoint,
		}

		switch endpoint.Type {
		case "ethereum":
			if err := client.db.Model(&sub).Related(&sub.Ethereum).Error; err != nil {
				log.Println(err)
				continue
			}
		}

		subs = append(subs, sub)
	}

	return subs, nil
}

func (client Client) SaveSubscription(sub *Subscription) error {
	if len(sub.EndpointName) == 0 {
		sub.EndpointName = sub.Endpoint.Name
	}
	e, _ := client.LoadEndpoint(sub.EndpointName)
	if e.Name != sub.EndpointName {
		return errors.New(fmt.Sprintf("Unable to get endpoint %s", sub.EndpointName))
	}
	return client.db.Create(sub).Error
}

func (client Client) LoadEndpoint(name string) (Endpoint, error) {
	var endpoint Endpoint
	err := client.db.Where(Endpoint{Name: name}).First(&endpoint).Error
	return endpoint, err
}

func (client Client) SaveEndpoint(endpoint *Endpoint) error {
	return client.db.Where(Endpoint{Name: endpoint.Name}).Assign(Endpoint{
		Url:        endpoint.Url,
		Type:       endpoint.Type,
		RefreshInt: endpoint.RefreshInt,
	}).FirstOrCreate(endpoint).Error
}

type Endpoint struct {
	gorm.Model
	Url        string `json:"url"`
	Type       string `json:"type"`
	RefreshInt int    `json:"refreshInterval"`
	Name       string `json:"name"`
}

type Subscription struct {
	gorm.Model
	ReferenceId  string
	Job          string
	EndpointName string
	Endpoint     Endpoint `gorm:"-"`
	Ethereum     EthSubscription
}

type EthSubscription struct {
	gorm.Model
	SubscriptionId uint
	Addresses      SQLStringArray
	Topics         SQLStringArray
}
