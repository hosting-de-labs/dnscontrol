package hostingde

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	hostingdeClient "github.com/hosting-de-labs/go-platform/client"
	hostingdeModel "github.com/hosting-de-labs/go-platform/model"

	"github.com/StackExchange/dnscontrol/v3/models"
	"github.com/StackExchange/dnscontrol/v3/pkg/diff"
	"github.com/StackExchange/dnscontrol/v3/providers"
)

type hostingdeProvider struct {
	Client *hostingdeClient.ApiClient
}

var features = providers.DocumentationNotes{
	providers.DocCreateDomains:       providers.Cannot(),
	providers.DocDualHost:            providers.Cannot(),
	providers.DocOfficiallySupported: providers.Cannot(),
	providers.CanUseTXTMulti:         providers.Can(),
	providers.CanGetZones:            providers.Can(),
	providers.CanUseAlias:            providers.Can(),
	providers.CanUseCAA:              providers.Can(),
	providers.CanUsePTR:              providers.Can(),
	providers.CanUseSRV:              providers.Can(),
	providers.CanUseTLSA:             providers.Can(),
	providers.CantUseNOPURGE:         providers.Cannot(),
}

func init() {
	providers.RegisterDomainServiceProviderType("HOSTINGDE", New, features)
}

// New creates a new API handle.
func New(settings map[string]string, _ json.RawMessage) (providers.DNSServiceProvider, error) {
	api := &hostingdeProvider{}
	if settings["apikey"] == "" {
		return nil, fmt.Errorf("missing apikey setting")
	}

	baseURL := "https://secure.hosting.de/api/"
	if settings["baseurl"] != "" {
		baseURL = settings["baseurl"]
	}

	limit := 10
	if settings["limit"] != "" {
		var err error
		limit, err = strconv.Atoi(settings["limit"])
		if err != nil {
			return nil, fmt.Errorf("limit setting can not be parsed to int")
		}
	}

	api.Client = hostingdeClient.NewApiClient(baseURL, settings["apikey"], limit)

	return api, nil
}

func toHostingdeRecord(r models.RecordConfig) *hostingdeModel.RecordObject {
	ro := &hostingdeModel.RecordObject{
		Name: r.NameFQDN,
		Type: r.Type,
		TTL:  int(r.TTL),
	}

	if r.Type == "SRV" {
		ro.Priority = int(r.SrvPriority)
		weight := strconv.Itoa(int(r.SrvWeight))
		port := strconv.Itoa(int(r.SrvPort))
		ro.Content = weight + " " + port + " " + r.Target
	} else if r.Type == "CAA" {
		flag := strconv.Itoa(int(r.CaaFlag))
		ro.Content = flag + " " + r.Target
	} else {
		ro.Priority = int(r.MxPreference)
		ro.Content = r.Target
	}
	return ro
}

func toDNSControlRecord(domain string, r hostingdeModel.RecordObject) *models.RecordConfig {

	rc := &models.RecordConfig{
		NameFQDN:     r.Name,
		Type:         r.Type,
		TTL:          uint32(r.TTL),
		MxPreference: uint16(r.Priority),
		SrvPriority:  uint16(0),
		SrvWeight:    uint16(0),
		SrvPort:      uint16(0),
		Original:     r,
	}

	if r.Type == "SRV" {
		parts := strings.Split(r.Content, " ")
		weight, _ := strconv.ParseUint(parts[0], 10, 16)
		port, _ := strconv.ParseUint(parts[1], 10, 16)
		rc.SrvPriority = uint16(r.Priority)
		rc.SrvWeight = uint16(weight)
		rc.SrvPort = uint16(port)
		_ = rc.SetTarget(parts[2])
	} else if r.Type == "CAA" {
		parts := strings.Split(r.Content, " ")
		caaFlag, _ := strconv.ParseUint(parts[0], 10, 32)
		rc.CaaFlag = uint8(caaFlag)
		rc.CaaTag = parts[1]
		_ = rc.SetTarget(strings.Trim(parts[2], "\""))
	} else {
		_ = rc.SetTarget(r.Content)
	}

	return rc
}

func (api *hostingdeProvider) GetZoneConfig(domain string) (*hostingdeModel.ZoneConfigObject, error) {
	filter := &hostingdeClient.RequestFilter{
		Field: "ZoneNameUnicode",
		Value: domain,
	}

	zoneConfigs, err := api.Client.Dns.ZoneConfigsFind(filter)
	if err != nil {
		return nil, err
	}
	if cap(zoneConfigs) != 1 {
		return nil, errors.New("ZoneConfig not found")
	}
	return &zoneConfigs[0], nil
}

func (api *hostingdeProvider) GetZone(domain string) (*hostingdeModel.ZoneObject, error) {
	filter := &hostingdeClient.RequestFilter{
		Field: "ZoneNameUnicode",
		Value: domain,
	}

	zones, err := api.Client.Dns.ZonesFind(filter)
	if err != nil {
		return nil, err
	}
	if cap(zones) != 1 {
		return nil, errors.New("Zone not found")
	}
	return &zones[0], nil
}

func (api *hostingdeProvider) GetZoneRecords(domain string) (models.Records, error) {
	zone, err := api.GetZone(domain)
	if err != nil {
		return nil, err
	}

	dcRecords := make([]*models.RecordConfig, len(zone.Records))
	for i := range zone.Records {
		dcRecords[i] = toDNSControlRecord(domain, zone.Records[i])
	}
	return dcRecords, nil
}

func (api *hostingdeProvider) GetNameservers(domain string) ([]*models.Nameserver, error) {
	zone, err := api.GetZone(domain)
	if err != nil {
		return nil, err
	}

	var nameservers []string
	for _, record := range zone.Records {
		if record.Type == "NS" {
			nameservers = append(nameservers, record.Content)
		}
	}

	return models.ToNameservers(nameservers)
}

// GetDomainCorrections returns the corrections for a domain.
func (api *hostingdeProvider) GetDomainCorrections(dc *models.DomainConfig) ([]*models.Correction, error) {
	dc, err := dc.Copy()
	if err != nil {
		return nil, err
	}

	dc.Punycode()
	domain := dc.Name

	// Check existing set
	existingRecords, err := api.GetZoneRecords(domain)
	if err != nil {
		return nil, err
	}

	// Normalize
	models.PostProcessRecords(existingRecords)
	differ := diff.New(dc)
	_, create, del, modify, err := differ.IncrementalDiff(existingRecords)
	if err != nil {
		return nil, err
	}

	var recordsToAdd []hostingdeModel.RecordObject
	for _, object := range create {
		record := object.Existing.Original.(*hostingdeModel.RecordObject)
		recordsToAdd = append(recordsToAdd, *record)
	}

	var recordsToModify []hostingdeModel.RecordObject
	for _, object := range modify {
		record := object.Existing.Original.(*hostingdeModel.RecordObject)
		recordsToModify = append(recordsToModify, *record)
	}

	var recordsToDelete []hostingdeModel.RecordObject
	for _, object := range del {
		record := object.Existing.Original.(*hostingdeModel.RecordObject)
		recordsToDelete = append(recordsToDelete, *record)
	}

	var corrections []*models.Correction

	// only create corrections if there are changes
	msg := fmt.Sprintf("Updating Zone")
	if cap(recordsToAdd) > 0 || cap(recordsToModify) > 0 || cap(recordsToDelete) > 0 {
		corrections = append(corrections,
			&models.Correction{
				Msg: msg,
				F: func() error {
					zoneConfig, err := api.GetZoneConfig(domain)
					if err != nil {
						return err
					}
					var updateRequest hostingdeClient.ZoneUpdateRequest
					updateRequest.ZoneConfig = *zoneConfig
					updateRequest.RecordsToAdd = recordsToAdd
					updateRequest.RecordsToModify = recordsToModify
					updateRequest.RecordsToDelete = recordsToDelete
					_, err = api.Client.Dns.ZoneUpdate(updateRequest)
					return err
				},
			})
	}

	return corrections, nil
}
