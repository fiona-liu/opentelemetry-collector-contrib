// Copyright  OpenTelemetry Authors
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

package redisclientreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisclientreceiver"

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/scrapererror"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisclientreceiver/internal/metadata"
)

type redisclientScraper struct {
	settings   component.TelemetrySettings
	cfg        *Config
	httpClient *http.Client
	mb         *metadata.MetricsBuilder
}

type response struct {
	Operation int `json:"OpsPerSecond"`
	LatencyPercentiles   map[string]float64 `json:"LatencyPercentiles"`
}


func newRedisclientScraper(
	settings component.ReceiverCreateSettings,
	cfg *Config,
) *redisclientScraper {
	return &redisclientScraper{
		settings: settings.TelemetrySettings,
		cfg:      cfg,
		mb:       metadata.NewMetricsBuilder(cfg.Metrics, settings.BuildInfo),
	}
}

func (r *redisclientScraper) start(_ context.Context, host component.Host) error {
	httpClient, err := r.cfg.ToClient(host.GetExtensions(), r.settings)
	if err != nil {
		return err
	}
	r.httpClient = httpClient
	return nil
}

func (r *redisclientScraper) scrape(context.Context) (pmetric.Metrics, error) {
	if r.httpClient == nil {
		return pmetric.Metrics{}, errors.New("failed to connect to HTTP client")
	}

	stats, err := r.GetStats()
	if err != nil {
		r.settings.Logger.Error("failed to fetch stats", zap.Error(err))
		return pmetric.Metrics{}, err
	}

	now := pcommon.NewTimestampFromTime(time.Now())

	var resp map[string]response
	json.Unmarshal(stats, &resp)

	for key, val := range resp {
		r.mb.RecordRedisClientOperationOpspersecondDataPoint(now, val.Operation, key)
		for percentile, latency := range val.LatencyPercentiles {
			switch percentile {
			case "p50.00":
				rs.mb.RecordRedisClientOperationP50latencyDataPoint(ts, float64(latency), key)
			case "p90.00":
				rs.mb.RecordRedisClientOperationP90latencyDataPoint(ts, float64(latency), key)
			case "p95.00":
				rs.mb.RecordRedisClientOperationP95latencyDataPoint(ts, float64(latency), key)
			case "p99.00":
				rs.mb.RecordRedisClientOperationP99latencyDataPoint(ts, float64(latency), key)
			case "p99.90":
				rs.mb.RecordRedisClientOperationP999latencyDataPoint(ts, float64(latency), key)
			case "p99.99":
				rs.mb.RecordRedisClientOperationP9999latencyDataPoint(ts, float64(latency), key)
			case "p100.0":
				rs.mb.RecordRedisClientOperationP100latencyDataPoint(ts, float64(latency), key)
			}
		}
	}


	md := pdata.NewMetrics()
	ilm := md.ResourceMetrics().AppendEmpty().InstrumentationLibraryMetrics().AppendEmpty()
	ilm.InstrumentationLibrary().SetName("otelcol/redisclient")
	r.mb.Emit(ilm.Metrics())
	return md, nil
}

// GetStats collects metric stats by making a get request to memtier benchmark API.
func (r *redisclientScraper) GetStats() (byte[], error) {
	resp, err := r.httpClient.Get(fmt.Sprintf("http://localhost:%s/api/lastminstats", r.cfg.Port)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return body, nil
}