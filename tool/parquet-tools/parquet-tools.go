package main

import (
	"flag"
	"fmt"

	"github.com/calmisland/parquet-go-source/local"
	"github.com/calmisland/parquet-go/reader"
	"github.com/calmisland/parquet-go/tool/parquet-tools/schematool"
	"github.com/calmisland/parquet-go/tool/parquet-tools/sizetool"
)

func main() {
	cmd := flag.String("cmd", "schema", "command to run. Allowed values: schema, rowcount or size")
	fileName := flag.String("file", "", "file name")
	withTags := flag.Bool("tag", false, "show struct tags")
	withPrettySize := flag.Bool("pretty", false, "show pretty size")
	uncompressedSize := flag.Bool("uncompressed", false, "show uncompressed size")

	flag.Parse()

	fr, err := local.NewLocalFileReader(*fileName)
	if err != nil {
		fmt.Println("Can't open file ", *fileName)
		return
	}

	pr, err := reader.NewParquetColumnReader(fr, 1)
	if err != nil {
		fmt.Println("Can't create parquet reader ", err)
		return
	}

	switch *cmd {
	case "schema":
		tree := schematool.CreateSchemaTree(pr.SchemaHandler.SchemaElements)
		fmt.Println("----- Go struct -----")
		fmt.Printf("%s\n", tree.OutputStruct(*withTags))
		fmt.Println("----- Json schema -----")
		fmt.Printf("%s\n", tree.OutputJsonSchema())
	case "rowcount":
		fmt.Println(pr.GetNumRows())
	case "size":
		fmt.Println(sizetool.GetParquetFileSize(*fileName, pr, *withPrettySize, *uncompressedSize))
	default:
		fmt.Println("Unknown command")
	}

}
