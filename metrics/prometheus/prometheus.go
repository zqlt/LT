
















package prometheus

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)


func Handler(reg metrics.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		
		var names []string
		reg.Each(func(name string, i interface{}) {
			names = append(names, name)
		})
		sort.Strings(names)

		
		c := newCollector()

		for _, name := range names {
			i := reg.Get(name)

			switch m := i.(type) {
			case metrics.Counter:
				c.addCounter(name, m.Snapshot())
			case metrics.Gauge:
				c.addGauge(name, m.Snapshot())
			case metrics.GaugeFloat64:
				c.addGaugeFloat64(name, m.Snapshot())
			case metrics.Histogram:
				c.addHistogram(name, m.Snapshot())
			case metrics.Meter:
				c.addMeter(name, m.Snapshot())
			case metrics.Timer:
				c.addTimer(name, m.Snapshot())
			case metrics.ResettingTimer:
				c.addResettingTimer(name, m.Snapshot())
			default:
				log.Warn("Unknown Prometheus metric type", "type", fmt.Sprintf("%T", i))
			}
		}
		w.Header().Add("Content-Type", "text/plain")
		w.Header().Add("Content-Length", fmt.Sprint(c.buff.Len()))
		w.Write(c.buff.Bytes())
	})
}
