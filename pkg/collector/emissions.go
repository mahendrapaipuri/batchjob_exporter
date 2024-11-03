//go:build !noemissions
// +build !noemissions

package collector

import (
	"context"
	"log/slog"
	"time"

	"github.com/mahendrapaipuri/ceems/pkg/emissions"
	"github.com/prometheus/client_golang/prometheus"
)

const emissionsCollectorSubsystem = "emissions"

type emissionsCollector struct {
	logger                   *slog.Logger
	emissionFactorProviders  emissions.FactorProviders
	emissionFactorMetricDesc *prometheus.Desc
	prevReadTime             int64
	prevEmissionFactors      map[string]float64
}

var newFactorProviders = emissions.NewFactorProviders

func init() {
	RegisterCollector(emissionsCollectorSubsystem, defaultDisabled, NewEmissionsCollector)
}

// NewEmissionsCollector returns a new Collector exposing emission factor metrics.
func NewEmissionsCollector(logger *slog.Logger) (Collector, error) {
	// Create metric description
	emissionsMetricDesc := prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, emissionsCollectorSubsystem, "gCo2_kWh"),
		"Current emission factor in CO2eq grams per kWh",
		[]string{"provider", "provider_name", "country_code", "country"}, nil,
	)

	// Create a new instance of EmissionCollector
	emissionFactorProviders, err := newFactorProviders(logger)
	if err != nil {
		logger.Error("Failed to create new EmissionCollector", "err", err)

		return nil, err
	}

	return &emissionsCollector{
		logger:                   logger,
		emissionFactorProviders:  *emissionFactorProviders,
		emissionFactorMetricDesc: emissionsMetricDesc,
		prevReadTime:             time.Now().Unix(),
		prevEmissionFactors:      make(map[string]float64),
	}, nil
}

// Update implements Collector and exposes emission factor.
func (c *emissionsCollector) Update(ch chan<- prometheus.Metric) error {
	currentEmissionFactors := c.emissionFactorProviders.Collect()
	// Returned value negative == emissions factor is not avail
	for provider, payload := range currentEmissionFactors {
		if payload.Factor != nil {
			for code, factor := range payload.Factor {
				if factor.Factor > 0 {
					ch <- prometheus.MustNewConstMetric(c.emissionFactorMetricDesc, prometheus.GaugeValue, float64(factor.Factor), provider, payload.Name, code, factor.Name)
				}
			}
		}
	}

	return nil
}

// Stops collector and releases system resources.
func (c *emissionsCollector) Stop(_ context.Context) error {
	c.logger.Debug("Stopping", "collector", emissionsCollectorSubsystem)

	return nil
}
