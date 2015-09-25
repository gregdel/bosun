package database

import (
	"bosun.org/collect"
	"bosun.org/opentsdb"
	"fmt"
	"strconv"

	"bosun.org/_third_party/github.com/garyburd/redigo/redis"
)

/*
Search data in redis:

Metrics by tags:
search:metrics:{tagk}={tagv} -> hash of metric name to timestamp

Tag keys by metric:
search:tagk:{metric} -> hash of tag key to timestamp

Tag Values By metric/tag key
search:tagv:{metric}:{tagk} -> hash of tag value to timestamp

*/
func searchMetricKey(tagK, tagV string) string {
	return fmt.Sprintf("search:metrics:%s=%s", tagK, tagV)
}

func (d *dataAccess) AddMetricForTag(tagK, tagV, metric string, time int64) error {
	defer collect.StartTimer("redis", opentsdb.TagSet{"op": "AddMetricForTag"})()
	conn := d.getConnection()
	defer conn.Close()

	_, err := conn.Do("HSET", searchMetricKey(tagK, tagV), metric, time)
	return err
}

func (d *dataAccess) GetMetricsForTag(tagK, tagV string) (map[string]int64, error) {
	defer collect.StartTimer("redis", opentsdb.TagSet{"op": "GetMetricsForTag"})()
	conn := d.getConnection()
	defer conn.Close()

	return stringInt64Map(conn.Do("HGETALL", searchMetricKey(tagK, tagV)))
}

func stringInt64Map(d interface{}, err error) (map[string]int64, error) {
	vals, err := redis.Strings(d, err)
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64)
	for i := 1; i < len(vals); i += 2 {
		time, _ := strconv.ParseInt(vals[i], 10, 64)
		result[vals[i-1]] = time
	}
	return result, err
}
