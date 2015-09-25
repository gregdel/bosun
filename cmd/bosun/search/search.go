package search // import "bosun.org/cmd/bosun/search"

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"bosun.org/cmd/bosun/database"
	"bosun.org/opentsdb"
	"bosun.org/slog"
)

// Search is a struct to hold indexed data about OpenTSDB metric and tag data.
// It is suited to answering questions about: available metrics for a tag set,
// available tag keys for a metric, and available tag values for a metric and
// tag key.
type Search struct {
	DataAccess database.DataAccess

	Last        map[string]*lastInfo
	updateTimes map[string]int64
	sync.RWMutex
}

type lastInfo struct {
	lastVal      float64
	diffFromPrev float64
	timestamp    int64
}

func NewSearch(data database.DataAccess) *Search {
	s := Search{
		DataAccess:  data,
		Last:        make(map[string]*lastInfo),
		updateTimes: make(map[string]int64),
	}
	return &s
}

func (s *Search) Index(mdp opentsdb.MultiDataPoint) {
	now := time.Now().Unix()

	for _, dp := range mdp {
		metric := dp.Metric
		key := metric + dp.Tags.String()
		needUpdate := false
		s.RLock()
		lastUpdate, ok := s.updateTimes[key]
		if !ok || now-lastUpdate > 10*60 { // update max every 10 minutes per metric/tagset
			needUpdate = true
		}
		s.RUnlock()
		if needUpdate {
			s.Lock()
			s.updateTimes[key] = now
			s.Unlock()
			for k, v := range dp.Tags {
				s.DataAccess.Search_AddMetricForTag(k, v, metric, now)
				s.DataAccess.Search_AddTagKeyForMetric(metric, k, now)
				s.DataAccess.Search_AddTagValue(metric, k, v, now)
				s.DataAccess.Search_AddTagValue(database.Search_All, k, v, now)
				s.DataAccess.Search_AddMetric(metric, now)
			}
		}

		s.Lock()
		p := s.Last[key]
		if p == nil {
			p = &lastInfo{}
			s.Last[key] = p
		}
		if p.timestamp < dp.Timestamp {
			if fv, err := getFloat(dp.Value); err == nil {
				p.diffFromPrev = (fv - p.lastVal) / float64(dp.Timestamp-p.timestamp)
				p.lastVal = fv
			} else {
				slog.Error(err)
			}
			p.timestamp = dp.Timestamp
		}
		s.Unlock()
	}

}

var floatType = reflect.TypeOf(float64(0))

func getFloat(unk interface{}) (float64, error) {
	v := reflect.ValueOf(unk)
	v = reflect.Indirect(v)
	if !v.Type().ConvertibleTo(floatType) {
		return 0, fmt.Errorf("cannot convert %v to float64", v.Type())
	}
	fv := v.Convert(floatType)
	return fv.Float(), nil
}

// Match returns all matching values against search. search is a regex, except
// that `.` is literal, `*` can be used for `.*`, and the entire string is
// searched (`^` and `&` added to ends of search).
func Match(search string, values []string) ([]string, error) {
	v := strings.Replace(search, ".", `\.`, -1)
	v = strings.Replace(v, "*", ".*", -1)
	v = "^" + v + "$"
	re, err := regexp.Compile(v)
	if err != nil {
		return nil, err
	}
	var nvs []string
	for _, nv := range values {
		if re.MatchString(nv) {
			nvs = append(nvs, nv)
		}
	}
	return nvs, nil
}

var errNotFloat = fmt.Errorf("last: expected float64")

// GetLast returns the value of the most recent data point for the given metric
// and tag. tags should be of the form "{key=val,key2=val2}". If diff is true,
// the value is treated as a counter. err is non nil if there is no match.
func (s *Search) GetLast(metric, tags string, diff bool) (v float64, err error) {
	s.RLock()
	p := s.Last[metric+tags]
	if p != nil {
		if diff {
			return p.diffFromPrev, nil
		}
		return p.lastVal, nil
	}
	s.RUnlock()
	return 0, nil
}

func (s *Search) Expand(q *opentsdb.Query) error {
	for k, ov := range q.Tags {
		var nvs []string
		for _, v := range strings.Split(ov, "|") {
			v = strings.TrimSpace(v)
			if v == "*" || !strings.Contains(v, "*") {
				nvs = append(nvs, v)
			} else {
				vs, err := s.TagValuesByMetricTagKey(q.Metric, k, 0)
				if err != nil {
					return err
				}
				ns, err := Match(v, vs)
				if err != nil {
					return err
				}
				nvs = append(nvs, ns...)
			}
		}
		if len(nvs) == 0 {
			return fmt.Errorf("expr: no tags matching %s=%s", k, ov)
		}
		q.Tags[k] = strings.Join(nvs, "|")
	}
	return nil
}

func (s *Search) UniqueMetrics() ([]string, error) {
	m, err := s.DataAccess.Search_GetAllMetrics()
	if err != nil {
		return nil, err
	}
	metrics := make([]string, len(m))
	i := 0
	for k, _ := range m {
		metrics[i] = k
		i++
	}
	sort.Strings(metrics)
	return metrics, nil
}

func (s *Search) TagValuesByTagKey(Tagk string, since time.Duration) ([]string, error) {
	return s.TagValuesByMetricTagKey(database.Search_All, Tagk, since)
}

func (s *Search) MetricsByTagPair(tagk, tagv string) ([]string, error) {
	metrics, err := s.DataAccess.Search_GetMetricsForTag(tagk, tagv)
	if err != nil {
		return nil, err
	}
	r := []string{}
	for k, _ := range metrics {
		r = append(r, k)
	}
	sort.Strings(r)
	return r, nil
}

func (s *Search) TagKeysByMetric(metric string) ([]string, error) {
	keys, err := s.DataAccess.Search_GetTagKeysForMetric(metric)
	if err != nil {
		return nil, err
	}
	r := []string{}
	for k := range keys {
		r = append(r, k)
	}
	sort.Strings(r)
	return r, nil
}

func (s *Search) TagValuesByMetricTagKey(metric, tagK string, since time.Duration) ([]string, error) {
	var t int64
	if since > 0 {
		t = time.Now().Add(-since).Unix()
	}
	vals, err := s.DataAccess.Search_GetTagValues(metric, tagK)
	if err != nil {
		return nil, err
	}
	r := []string{}
	for k, ts := range vals {
		if t <= ts {
			r = append(r, k)
		}
	}
	sort.Strings(r)
	return r, nil
}
