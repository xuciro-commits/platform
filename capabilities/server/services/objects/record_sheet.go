package objects

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"io"
	"path"
	"platformserver/kernel/protocol"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func ReadSheet(body io.Reader) (protocol.ImportSheet, *platform.Error) {
	data, err := io.ReadAll(io.LimitReader(body, 16<<20))
	rows, ok := sheetRows(data)
	if err != nil || !ok || len(rows) == 0 {
		return protocol.ImportSheet{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: "The file is not a CSV file or an Excel workbook"}
	}
	sheet := protocol.ImportSheet{Columns: rows[0], Rows: [][]string{}}
	for i := range sheet.Columns {
		sheet.Columns[i] = strings.TrimSpace(sheet.Columns[i])
	}
	for _, row := range rows[1:] {
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		line := make([]string, len(sheet.Columns))
		copy(line, row)
		sheet.Rows = append(sheet.Rows, line)
	}
	return sheet, nil
}

func sheetRows(data []byte) ([][]string, bool) {
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return workbookRows(data)
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	return rows, err == nil
}

type workbookText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (w workbookText) String() string {
	out := w.Text
	for _, run := range w.Runs {
		out += run.Text
	}
	return out
}

func workbookRows(data []byte) ([][]string, bool) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, false
	}
	part := func(name string, into any) bool {
		file, err := archive.Open(name)
		if err != nil {
			return false
		}
		defer file.Close()
		return xml.NewDecoder(io.LimitReader(file, 64<<20)).Decode(into) == nil
	}
	var book struct {
		Sheets []struct {
			ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	var links struct {
		Links []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if !part("xl/workbook.xml", &book) || len(book.Sheets) == 0 || !part("xl/_rels/workbook.xml.rels", &links) {
		return nil, false
	}
	first := ""
	for _, link := range links.Links {
		if link.ID == book.Sheets[0].ID {
			first = strings.TrimPrefix(link.Target, "/")
			if !strings.HasPrefix(first, "xl/") {
				first = path.Join("xl", first)
			}
		}
	}
	var shared struct {
		Items []workbookText `xml:"si"`
	}
	part("xl/sharedStrings.xml", &shared)
	var sheet struct {
		Rows []struct {
			Cells []struct {
				Ref    string       `xml:"r,attr"`
				Type   string       `xml:"t,attr"`
				Value  string       `xml:"v"`
				Inline workbookText `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if first == "" || !part(first, &sheet) {
		return nil, false
	}
	rows := [][]string{}
	for _, row := range sheet.Rows {
		line := []string{}
		for i, cell := range row.Cells {
			column := i
			if letters := strings.TrimRight(cell.Ref, "0123456789"); letters != "" {
				column = 0
				for _, letter := range letters {
					column = column*26 + int(letter-'A'+1)
				}
				column--
			}
			for len(line) <= column {
				line = append(line, "")
			}
			value := cell.Value
			switch cell.Type {
			case "s":
				index, err := strconv.Atoi(value)
				if err != nil || index < 0 || index >= len(shared.Items) {
					return nil, false
				}
				value = shared.Items[index].String()
			case "inlineStr":
				value = cell.Inline.String()
			case "b":
				value = strconv.FormatBool(value == "1")
			}
			line[column] = value
		}
		rows = append(rows, line)
	}
	return rows, true
}
