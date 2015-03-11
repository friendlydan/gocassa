package gocassa

import (
	"bytes"
	"fmt"
	r "github.com/hailocab/gocassa/reflect"
	"reflect"
	"strconv"
	"strings"
)

type t struct {
	keySpace *k
	info     *tableInfo
}

// Contains mostly analyzed information about the entity
type tableInfo struct {
	keyspace, name string
	marshalSource  interface{}
	fieldSource    map[string]interface{}
	keys           Keys
	fieldNames     map[string]struct{} // This is here only to check containment
	fields         []string
	fieldValues    []interface{}
}

func newTableInfo(keyspace, name string, keys Keys, entity interface{}, fieldSource map[string]interface{}) *tableInfo {
	cinf := &tableInfo{
		keyspace:      keyspace,
		name:          name,
		marshalSource: entity,
		keys:          keys,
		fieldSource:   fieldSource,
	}
	fields := []string{}
	values := []interface{}{}
	for k, v := range fieldSource {
		fields = append(fields, k)
		values = append(values, v)
	}
	cinf.fieldNames = map[string]struct{}{}
	for _, v := range fields {
		cinf.fieldNames[v] = struct{}{}
	}
	cinf.fields = fields
	cinf.fieldValues = values
	return cinf
}

func (t *t) zero() interface{} {
	return reflect.New(reflect.TypeOf(t.info.marshalSource)).Interface()
}

// Since we cant have Map -> [(k, v)] we settle for Map -> ([k], [v])
// #tuplelessLifeSucks
func keyValues(m map[string]interface{}) ([]string, []interface{}) {
	keys := []string{}
	values := []interface{}{}
	for k, v := range m {
		keys = append(keys, k)
		values = append(values, v)
	}
	return keys, values
}

func toMap(i interface{}) (map[string]interface{}, bool) {
	switch v := i.(type) {
	//case M:
	//	return map[string]interface{}(v), true
	case map[string]interface{}:
		return v, true
	}
	return r.StructToMap(i)
}

func (t t) Where(rs ...Relation) Filter {
	return filter{
		t:  t,
		rs: rs,
	}
}

func (t t) generateFieldNames() string {
	xs := make([]string, len(t.info.fields))
	for i, v := range t.info.fields {
		xs[i] = strings.ToLower(v)
	}
	return strings.Join(xs, ", ")
}

func relations(keys Keys, m map[string]interface{}) []Relation {
	ret := []Relation{}
	for _, v := range append(keys.PartitionKeys, keys.ClusteringColumns...) {
		ret = append(ret, Eq(v, m[v]))
	}
	return ret
}

func removeFields(m map[string]interface{}, s []string) map[string]interface{} {
	keys := map[string]bool{}
	for _, v := range s {
		keys[v] = true
	}
	ret := map[string]interface{}{}
	for k, v := range m {
		if !keys[k] {
			ret[k] = v
		}
	}
	return ret
}

// INSERT INTO Hollywood.NerdMovies (user_uuid, fan)
//   VALUES ('cfd66ccc-d857-4e90-b1e5-df98a3d40cd6', 'johndoe')
//
// Gotcha: primkey must be first
func insertStatement(keySpaceName, cfName string, fieldNames []string, opts Options) string {
	placeHolders := make([]string, len(fieldNames))
	for i := 0; i < len(fieldNames); i++ {
		placeHolders[i] = "?"
	}
	lowerFieldNames := make([]string, len(fieldNames))
	for i, v := range fieldNames {
		lowerFieldNames[i] = strings.ToLower(v)
	}

	buf := new(bytes.Buffer)
	buf.WriteString(fmt.Sprintf("INSERT INTO %s.%s (%s) VALUES (%s)",
		keySpaceName,
		cfName,
		strings.Join(lowerFieldNames, ", "),
		strings.Join(placeHolders, ", ")))

	// Apply options
	if opts.TTL != 0 {
		buf.WriteString(" USING TTL ")
		buf.WriteString(strconv.FormatFloat(opts.TTL.Seconds(), 'f', 0, 64))
	}

	return buf.String()
}

func (t t) SetWithOptions(i interface{}, opts Options) Op {
	m, ok := toMap(i)
	if !ok {
		panic("SetWithOptions: Incompatible type")
	}
	ks := append(t.info.keys.PartitionKeys, t.info.keys.ClusteringColumns...)
	updFields := removeFields(m, ks)
	if len(updFields) == 0 {
		fields, insertVals := keyValues(m)
		insertStmt := insertStatement(t.keySpace.name, t.info.name, fields, opts)
		if t.keySpace.debugMode {
			fmt.Println(insertStmt, insertVals)
		}
		return newWriteOp(t.keySpace.qe, insertStmt, insertVals)
	}
	updStmt, updVals := updateStatement(t.keySpace.name, t.info.name, updFields, opts)
	whereStmt, whereVals := generateWhere(relations(t.info.keys, m))
	if t.keySpace.debugMode {
		fmt.Println(updStmt+whereStmt, append(updVals, whereVals...))
	}
	return newWriteOp(t.keySpace.qe, updStmt+whereStmt, append(updVals, whereVals...))
}

func (t t) Set(i interface{}) Op {
	return t.SetWithOptions(i, Options{})
}

func (t t) Create() error {
	if stmt, err := t.CreateStatement(); err != nil {
		return err
	} else {
		return t.keySpace.qe.Execute(stmt)
	}
}

// Drop table if exists and create it again
func (t t) Recreate() error {
	if ex, err := t.keySpace.Exists(t.info.name); ex && err == nil {
		if err := t.keySpace.DropTable(t.info.name); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return t.Create()
}

func (t t) CreateStatement() (string, error) {
	return createTable(t.keySpace.name,
		t.info.name,
		t.info.keys.PartitionKeys,
		t.info.keys.ClusteringColumns,
		t.info.fields,
		t.info.fieldValues)
}

func (t t) Name() string {
	return t.info.name
}

//const (
//	asc	 = iota
//	desc
//)
//
//type Ordering struct {
//	fieldName string
//	order int
//}
//
//func ASC(fieldName string) Ordering {
//	return Ordering{
//		fieldName: fieldName,
//		order: asc,
//	}
//}
//
//func DESC(fieldName string) Ordering {
//	return Ordering{
//		fieldName: fieldName,
//		order: asc,
//	}
//}
