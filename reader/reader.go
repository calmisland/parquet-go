package reader

import (
	"encoding/binary"
	"io"
	"reflect"
	"sync"

	"github.com/apache/thrift/lib/go/thrift"
	"github.com/calmisland/parquet-go/common"
	"github.com/calmisland/parquet-go/layout"
	"github.com/calmisland/parquet-go/marshal"
	"github.com/calmisland/parquet-go/source"
	"github.com/calmisland/parquet-go/schema"
	"github.com/calmisland/parquet-go/parquet"
)

type ParquetReader struct {
	SchemaHandler *schema.SchemaHandler
	NP            int64 //parallel number
	Footer        *parquet.FileMetaData
	PFile         source.ParquetFile

	ColumnBuffers map[string]*ColumnBufferType
}

//Create a parquet reader
func NewParquetReader(pFile source.ParquetFile, obj interface{}, np int64) (*ParquetReader, error) {
	var err error
	res := new(ParquetReader)
	res.NP = np
	res.PFile = pFile
	if err = res.ReadFooter(); err != nil {
		return nil, err
	}
	res.ColumnBuffers = make(map[string]*ColumnBufferType)

	if obj != nil {
		if res.SchemaHandler, err = schema.NewSchemaHandlerFromStruct(obj); err != nil {
			return res, err
		}
		res.RenameSchema()

		for i := 0; i < len(res.SchemaHandler.SchemaElements); i++ {
			schema := res.SchemaHandler.SchemaElements[i]
			if schema.GetNumChildren() == 0 {
				pathStr := res.SchemaHandler.IndexMap[int32(i)]
				if res.ColumnBuffers[pathStr], err = NewColumnBuffer(pFile, res.Footer, res.SchemaHandler, pathStr); err != nil {
					return res, err
				}
			}
		}
	}
	return res, nil
}

func (self *ParquetReader) SetSchemaHandlerFromJSON(jsonSchema string) error {
	var err error
	if self.SchemaHandler, err = schema.NewSchemaHandlerFromJSON(jsonSchema); err != nil {
		return err
	}
	self.RenameSchema()
	for i := 0; i < len(self.SchemaHandler.SchemaElements); i++ {
		schemaElement := self.SchemaHandler.SchemaElements[i]
		if schemaElement.GetNumChildren() == 0 {
			pathStr := self.SchemaHandler.IndexMap[int32(i)]
			if self.ColumnBuffers[pathStr], err = NewColumnBuffer(self.PFile, self.Footer, self.SchemaHandler, pathStr); err != nil {
				return err
			}
		}
	}
	return nil
}

//Rename schema name to inname
func (self *ParquetReader) RenameSchema() {
	for i := 0; i < len(self.SchemaHandler.Infos); i++ {
		self.Footer.Schema[i].Name = self.SchemaHandler.Infos[i].InName
	}
	for _, rowGroup := range self.Footer.RowGroups {
		for _, chunk := range rowGroup.Columns {
			exPath := make([]string, 0)
			exPath = append(exPath, self.SchemaHandler.GetRootName())
			exPath = append(exPath, chunk.MetaData.GetPathInSchema()...)
			exPathStr := common.PathToStr(exPath)

			inPathStr := self.SchemaHandler.ExPathToInPath[exPathStr]
			inPath := common.StrToPath(inPathStr)[1:]
			chunk.MetaData.PathInSchema = inPath
		}
	}
}

func (self *ParquetReader) GetNumRows() int64 {
	return self.Footer.GetNumRows()
}

//Get the footer size
func (self *ParquetReader) GetFooterSize() (uint32, error) {
	var err error
	buf := make([]byte, 4)
	if _, err = self.PFile.Seek(-8, io.SeekEnd); err != nil {
		return 0, err
	}
	if _, err = self.PFile.Read(buf); err != nil {
		return 0, err
	}
	size := binary.LittleEndian.Uint32(buf)
	return size, err
}

//Read footer from parquet file
func (self *ParquetReader) ReadFooter() error {
	size, err := self.GetFooterSize()
	if err != nil {
		return err
	}
	if _, err = self.PFile.Seek(-(int64)(8+size), io.SeekEnd); err != nil {
		return err
	}
	self.Footer = parquet.NewFileMetaData()
	pf := thrift.NewTCompactProtocolFactory()
	protocol := pf.GetProtocol(thrift.NewStreamTransportR(self.PFile))
	return self.Footer.Read(protocol)
}

//Skip rows of parquet file
func (self *ParquetReader) SkipRows(num int64) error {
	var err error
	if num <= 0 {
		return nil
	}
	doneChan := make(chan int, self.NP)
	taskChan := make(chan string, len(self.SchemaHandler.ValueColumns))
	stopChan := make(chan int)

	for _, pathStr := range self.SchemaHandler.ValueColumns {
		if _, ok := self.ColumnBuffers[pathStr]; !ok {
			if self.ColumnBuffers[pathStr], err = NewColumnBuffer(self.PFile, self.Footer, self.SchemaHandler, pathStr); err != nil {
				return err
			}
		}
	}

	for i := int64(0); i < self.NP; i++ {
		go func() {
			for {
				select {
				case <-stopChan:
					return
				case pathStr := <-taskChan:
					cb := self.ColumnBuffers[pathStr]
					cb.SkipRows(int64(num))
					doneChan <- 0
				}
			}
		}()
	}

	for key, _ := range self.ColumnBuffers {
		taskChan <- key
	}

	for i := 0; i < len(self.ColumnBuffers); i++ {
		<-doneChan
	}
	for i := int64(0); i < self.NP; i++ {
		stopChan <- 0
	}
	return err
}

//Read rows of parquet file
func (self *ParquetReader) Read(dstInterface interface{}) error {
	var err error
	tmap := make(map[string]*layout.Table)
	locker := new(sync.Mutex)
	ot := reflect.TypeOf(dstInterface).Elem().Elem()
	num := reflect.ValueOf(dstInterface).Elem().Len()
	if num <= 0 {
		return nil
	}

	doneChan := make(chan int, self.NP)
	taskChan := make(chan string, len(self.ColumnBuffers))
	stopChan := make(chan int)

	for i := int64(0); i < self.NP; i++ {
		go func() {
			for {
				select {
				case <-stopChan:
					return
				case pathStr := <-taskChan:
					cb := self.ColumnBuffers[pathStr]
					table, _ := cb.ReadRows(int64(num))
					locker.Lock()
					if _, ok := tmap[pathStr]; ok {
						tmap[pathStr].Merge(table)
					} else {
						tmap[pathStr] = layout.NewTableFromTable(table)
						tmap[pathStr].Merge(table)
					}
					locker.Unlock()
					doneChan <- 0
				}
			}
		}()
	}
	for key, _ := range self.ColumnBuffers {
		taskChan <- key
	}
	for i := 0; i < len(self.ColumnBuffers); i++ {
		<-doneChan
	}
	for i := int64(0); i < self.NP; i++ {
		stopChan <- 0
	}

	dstList := make([]interface{}, self.NP)
	delta := (int64(num) + self.NP - 1) / self.NP

	doneChan = make(chan int)
	for c := int64(0); c < self.NP; c++ {
		bgn := c * delta
		end := bgn + delta
		if end > int64(num) {
			end = int64(num)
		}
		if bgn >= int64(num) {
			bgn, end = int64(num), int64(num)
		}
		go func(b, e, index int) {
			dstList[index] = reflect.New(reflect.SliceOf(ot)).Interface()
			if err2 := marshal.Unmarshal(&tmap, b, e, dstList[index], self.SchemaHandler); err2 != nil {
				err = err2
			}
			doneChan <- 0
		}(int(bgn), int(end), int(c))
	}
	for c := int64(0); c < self.NP; c++ {
		<-doneChan
	}

	resTmp := reflect.MakeSlice(reflect.SliceOf(ot), 0, num)
	for _, dst := range dstList {
		resTmp = reflect.AppendSlice(resTmp, reflect.ValueOf(dst).Elem())
	}
	reflect.ValueOf(dstInterface).Elem().Set(resTmp)
	return err
}

//Stop Read
func (self *ParquetReader) ReadStop() {
	for _, cb := range self.ColumnBuffers {
		if cb != nil {
			cb.PFile.Close()
		}
	}
}
