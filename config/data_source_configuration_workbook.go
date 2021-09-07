package config

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/360EntSecGroup-Skylar/excelize/v2"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func dataSourceConfigurationWorkbook() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceConfigurationItemRead,
		Schema: map[string]*schema.Schema{
			"json": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"csv": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"schema": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"excel": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"worksheet": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"orientation": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "horizontal",
			},
			"col_start": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"col_end": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"col_config_item": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"configuration_item": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"filter": dataSourceFilterSchema(),
		},
	}
}

func dataSourceConfigurationItemRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	// get all arguments passed to the resource
	csv_string := d.Get("csv").(string)
	config_schema := d.Get("schema").(string)
	configuration_item := d.Get("configuration_item").(string)
	col_config_item := d.Get("col_config_item").(string)
	excel_file := d.Get("excel").(string)
	sheet_name := d.Get("worksheet").(string)
	orientation := d.Get("orientation").(string)
	start_column := d.Get("col_start").(string)
	end_column := d.Get("col_end").(string)

	var filters []map[string]interface{}

	// gather all filters
	if v, ok := d.GetOk("filter"); ok {
		filters = buildConfigDataSourceFilters(v.(*schema.Set))
	}

	// set the default configuration item column name
	if col_config_item == "" {
		col_config_item = "configuration_item"
	}

	// set default value for configuration_item
	if configuration_item == "" && sheet_name != "" {
		configuration_item = sheet_name
	}

	// ###### Start Validations ######

	// make sure csv or excel is used
	if csv_string == "" && excel_file == "" {
		return diag.FromErr(fmt.Errorf(fmt.Sprintf("%v", "Must use csv or excel on the resource")))
	}

	// make sure csv and excel is not on the same resource
	if csv_string != "" && excel_file != "" {
		return diag.FromErr(fmt.Errorf(fmt.Sprintf("%v", "Cannot use csv and excel on the same resource")))
	}

	orientation = strings.ToLower(orientation)
	valid_vertical_orientation := []string{"vertical", "vert", "v"}
	valid_horizontal_orientation := []string{"horizontal", "horiz", "h"}
	if stringInList(orientation, valid_vertical_orientation) {
		orientation = "vertical"
	} else if stringInList(orientation, valid_horizontal_orientation) {
		orientation = "horizontal"
	} else {
		return diag.FromErr(fmt.Errorf(fmt.Sprintf("%v", "Invalid type. Valid values are horizontal,vertical")))
	}

	if orientation == "vertical" && configuration_item == "" {
		return diag.FromErr(fmt.Errorf(fmt.Sprintf("%v", "configuration_item is required if type is vertical")))
	}

	if orientation == "vertical" && csv_string != "" {
		return diag.FromErr(fmt.Errorf(fmt.Sprintf("%v", "vertical orientation is only valid for excel")))
	}

	// ###### End Validations ######

	// check if excel is being used
	if excel_file != "" {
		csvstring, err := excelToCSV(excel_file, sheet_name, start_column, end_column, configuration_item, col_config_item, orientation)
		if err != nil {
			return diag.FromErr(fmt.Errorf(fmt.Sprintf("%v", csvstring)))
		}
		csv_string = csvstring
		// return diag.FromErr(fmt.Errorf(fmt.Sprintf("%v", csvstring)))
	}

	if csv_string != "" {
		// convert the csv to map
		// var csv []map[string]string
		csv, err := stringToMap(csv_string)
		if err != nil {
			return diag.FromErr(err)
		}

		// get all unique configuration items
		items := unique(getConfigurationItems(csv, col_config_item))

		// convert the schema to map
		var map_yaml interface{}
		if config_schema != "" {
			map_yaml, err = stringToInterface(config_schema)
			if err != nil {
				return diag.FromErr(err)
			}
		} else {
			map_yaml, err = createDefaultMapping(items, csv, col_config_item)
			if err != nil {
				return diag.FromErr(err)
			}
		}
		mapping := map_yaml.(map[interface{}]interface{})

		// remap all csv headers based on mapping configuration
		records := reMapData(csv, mapping["config_schema"], filters, col_config_item)

		// get the transformed data
		data := getItemData(records, items, col_config_item)

		// set the data to the attribute json
		if err := d.Set("json", data); err != nil {
			return diag.FromErr(err)
		}
	} else {
		// set the data to the attribute json
		if configuration_item == "" {
			configuration_item = sheet_name
		}
		if err := d.Set("json", "{\""+configuration_item+"\": []}"); err != nil {
			return diag.FromErr(err)
		}
	}

	d.SetId(strconv.FormatInt(time.Now().Unix(), 10))

	return diags
}

func excelToCSV(excel_file string, sheet_name string, start_column string, end_column string, configuration_item string, col_config_item string, orientation string) (string, error) {
	var row_arr = []string{
		"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z",
		"AA", "AB", "AC", "AD", "AE", "AF", "AG", "AH", "AI", "AJ", "AK", "AL", "AM", "AN", "AO", "AP", "AQ", "AR", "AS", "AT", "AU", "AV", "AW", "AX", "AY", "AZ",
	}
	min := 0
	max := len(row_arr) - 1
	if start_column != "" {
		min = sliceIndex(row_arr, start_column)
		if min == -1 {
			min = 0
		}
	}
	if end_column != "" {
		max = sliceIndex(row_arr, end_column)
		if max == -1 {
			max = len(row_arr) - 1
		}
	}
	var csv = []string{}

	f, err := excelize.OpenFile(excel_file)
	if err != nil {
		return "", err
	}

	// Get all rows
	rows, err := f.GetRows(sheet_name)
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("%v", rows))
	}

	// get the number of columns
	row_len := len(rows[0])

	if orientation == "horizontal" {
		// check if configuration item is in the column names
		config_item_exist := false
		for i := 0; i < row_len; i++ {
			if (i >= min) && (i <= max) && (i < row_len) {
				if (rows[0][i] == "configuration_item") || (rows[0][i] == col_config_item) {
					config_item_exist = true
				}
			}
		}

		for idx, row := range rows {
			var sb strings.Builder
			for i := 0; i < row_len; i++ {
				if (i >= min) && (i <= max) {
					if !config_item_exist {
						if idx == 0 && i == min {
							sb.WriteString("\"configuration_item\",")
						} else if idx > 0 && i == min {
							sb.WriteString("\"" + configuration_item + "\",")
						}
					}

					if i >= len(row) {
						sb.WriteString("")
					} else {
						sb.WriteString("\"" + row[i] + "\"")
					}
					if (i < row_len-1) && (i < max) {
						sb.WriteString(",")
					}
				}
			}

			// only accept rows that are not empty
			replacer := strings.NewReplacer(",", "", " ", "", "[]", "", "{}", "", "\"", "")
			if replacer.Replace(sb.String()) != "" {
				csv = append(csv, sb.String())
			}
		}
	} else {
		// Get total number of columns
		maxcol := 0
		for _, row := range rows {
			if len(row) > maxcol {
				maxcol = len(row)
			}
		}
		var sb strings.Builder
		sb.WriteString("\"configuration_item\",")
		fieldcount := 0
		for idx, row := range rows {
			if strings.Trim(row[0], " ") != "" {
				if idx == len(rows)-1 {
					sb.WriteString("\"" + row[0] + "\"")
				} else {
					sb.WriteString("\"" + row[0] + "\",")
				}
				fieldcount++
			}
		}
		csv = append(csv, sb.String())
		for i := 1; i < maxcol; i++ {
			sb.Reset()
			sb.WriteString("\"" + configuration_item + "\",")
			for idx, row := range rows {
				if i > len(row)-1 {
					if idx < len(rows)-1 {
						sb.WriteString("\"\",")
					} else {
						sb.WriteString("\"\"")
					}
				} else {
					if idx < len(rows)-1 {
						sb.WriteString("\"" + row[i] + "\",")
					} else {
						sb.WriteString("\"" + row[i] + "\"")
					}
				}
			}
			// only accept rows that are not empty
			replacer := strings.NewReplacer(",", "", " ", "", "[]", "", "{}", "", "\"", "")
			if replacer.Replace(sb.String()) != "" {
				csv = append(csv, sb.String())
			}
		}
	}
	if len(csv) == 1 {
		return "", err
	}
	// return strings.Join(csv, "\n"), fmt.Errorf(fmt.Sprintf("%v", csv))
	return strings.Join(csv, "\n"), err
}

func sliceIndex(arr []string, s string) int {
	for i, v := range arr {
		if v == s {
			return i
		}
	}
	return -1
}

func getConfigurationItems(csv []map[string]string, configuration_item string) []string {
	var items []string
	for _, value := range csv {
		for k, v := range value {
			if k == configuration_item {
				items = append(items, v)
			}
		}
	}
	return items
}

func getItemData(csv []map[string]interface{}, items []string, configuration_item string) string {
	listitem := make(map[string][]map[string]interface{})
	if len(items) > 0 {
		for _, item := range items {
			var itemdatalist []map[string]interface{}
			for _, value := range csv {
				if value[configuration_item] == item {
					itemdata := make(map[string]interface{})
					for k, v := range value {
						if k != configuration_item {
							itemdata[k] = v
						}
					}
					itemdatalist = append(itemdatalist, itemdata)
					listitem[item] = itemdatalist
				}
			}
		}
	} else {
		var itemdatalist []map[string]interface{}
		for _, value := range csv {
			itemdata := make(map[string]interface{})
			for k, v := range value {
				itemdata[k] = v
			}
			itemdatalist = append(itemdatalist, itemdata)
			listitem[configuration_item] = itemdatalist
		}
	}
	j, _ := json.Marshal(listitem)
	return string(j)
}

func unique(items []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range items {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func reMapData(csv []map[string]string, mapping interface{}, filters []map[string]interface{}, configuration_item string) []map[string]interface{} {
	new_csv := make([]map[string]interface{}, len(csv))
	for key, value := range csv {
		item_key := ""
		for k, v := range value {
			if k == configuration_item {
				item_key = v
			}
		}
		new_value := make(map[string]interface{})
		new_tag := make(map[string]string)
		tags := make(map[string]string)
		include_value := false
		var new_key, new_type string
		for k, v := range value {
			_ = v
			if strings.HasPrefix(k, "attr") {
				new_key, new_type = getMapValue(mapping, item_key, k)
				if new_key != "" {
					if new_type == "string" {
						new_value[new_key] = value[k]
					} else if new_type == "number" || new_type == "numeric" {
						n, _ := strconv.ParseFloat(value[k], 64)
						new_value[new_key] = n
					} else if new_type == "boolean" || new_type == "bool" {
						val, _ := strconv.ParseBool(value[k])
						new_value[new_key] = val
					} else if new_type == "list" {
						new_value[new_key] = strings.Split(value[k], ",")
					} else if new_type == "map" || new_type == "hash" {
						vlist := strings.Split(value[k], ",")
						vmap := make(map[string]string)
						for _, vl := range vlist {
							mlist := strings.Split(vl, "=")
							vmap[mlist[0]] = mlist[1]
						}
						new_value[new_key] = vmap
					} else if new_type == "tag" {
						new_tag[strings.Title(new_key)] = value[k]
					} else {
						new_value[new_key] = value[k]
					}
				}
			} else if k == configuration_item {
				new_value[k] = value[k]
			} else if strings.HasPrefix(k, "s_") || strings.HasPrefix(k, "string_") {
				replacer := strings.NewReplacer("s_", "", "string_", "")
				new_key := replacer.Replace(k)
				if value[k] != "" {
					new_value[new_key] = value[k]
				} else {
					new_value[new_key] = ""
				}
			} else if strings.HasPrefix(k, "t_") || strings.HasPrefix(k, "tag_") {
				replacer := strings.NewReplacer("t_", "", "tag_", "")
				new_key := strings.Title(replacer.Replace(k))
				if value[k] != "" {
					new_tag[new_key] = value[k]
				} else {
					new_tag[new_key] = ""
				}
			} else if strings.HasPrefix(k, "n_") || strings.HasPrefix(k, "num_") || strings.HasPrefix(k, "number_") || strings.HasPrefix(k, "numeric_") {
				replacer := strings.NewReplacer("n_", "", "num_", "", "number_", "", "numeric_", "")
				new_key := replacer.Replace(k)
				if value[k] != "" {
					n, _ := strconv.ParseFloat(value[k], 64)
					new_value[new_key] = n
				} else {
					new_value[new_key] = 0
				}
			} else if strings.HasPrefix(k, "b_") || strings.HasPrefix(k, "bool_") || strings.HasPrefix(k, "boolean_") {
				replacer := strings.NewReplacer("b_", "", "bool_", "", "boolean_", "")
				new_key := replacer.Replace(k)
				if value[k] != "" {
					val, _ := strconv.ParseBool(value[k])
					new_value[new_key] = val
				} else {
					new_value[new_key] = false
				}
			} else if strings.HasPrefix(k, "l_") || strings.HasPrefix(k, "list_") {
				replacer := strings.NewReplacer("l_", "", "list_", "")
				new_key := replacer.Replace(k)
				if value[k] != "" {
					new_value[new_key] = strings.Split(value[k], ",")
				} else {
					new_value[new_key] = []string{}
				}
			} else if strings.HasPrefix(k, "m_") || strings.HasPrefix(k, "map_") || strings.HasPrefix(k, "h_") || strings.HasPrefix(k, "hash_") {
				replacer := strings.NewReplacer("m_", "", "map_", "", "h_", "", "hash_", "")
				new_key := replacer.Replace(k)
				if value[k] != "" {
					vlist := strings.Split(value[k], ",")
					vmap := make(map[string]string)
					for _, vl := range vlist {
						mlist := strings.Split(vl, "=")
						vmap[mlist[0]] = mlist[1]
					}
					new_value[new_key] = vmap
				} else {
					new_value[new_key] = map[string]string{}
				}
			} else {
				new_value[k] = value[k]
			}

			// check if value included in filter
			if len(filters) > 0 {
				if !include_value {
					include_value = checkFiltersForItem(filters, k, value[k])
				}
				if !include_value {
					include_value = checkFiltersForItem(filters, new_key, value[k])
				}
			} else {
				include_value = true
			}
		}
		for k, v := range new_tag {
			tags[k] = v
		}
		if include_value {
			new_value["tags"] = tags
			new_csv[key] = new_value
		}
	}
	return new_csv
}

func getMapValue(config interface{}, config_item string, config_key string) (string, string) {
	return_value := config_key
	return_type := "string"
	item := config.(map[interface{}]interface{})
	for _, key := range reflect.ValueOf(item).MapKeys() {
		if key.Interface().(string) == config_item {
			item_map := item[key.Interface()].(map[interface{}]interface{})
			for _, ik := range reflect.ValueOf(item_map).MapKeys() {
				if ik.Interface().(string) == config_key {
					return_type = "string"
					kv, ok := item_map[ik.Interface()].(map[interface{}]interface{})
					if !ok {
						return_value = item_map[ik.Interface()].(string)
					} else {
						for _, ikv := range reflect.ValueOf(kv).MapKeys() {
							if ikv.Interface().(string) == "name" {
								return_value = kv[ikv.Interface()].(string)
							}
							if ikv.Interface().(string) == "type" {
								return_type = kv[ikv.Interface()].(string)
							}
						}
					}
				}
			}
		}
	}
	return return_value, return_type
}

func createDefaultMapping(items []string, csv []map[string]string, configuration_item string) (map[interface{}]interface{}, error) {
	mapping := make(map[interface{}]interface{})
	item_map := make(map[interface{}]interface{})
	var item_list []string

	if len(items) > 0 {
		item_list = items
	} else {
		item_list = append(item_list, configuration_item)
	}

	for _, s := range item_list {
		item := make(map[interface{}]interface{})
		for k := range csv[0] {
			if len(items) > 0 {
				if k != configuration_item {
					item[k] = k
				}
			} else {
				item[k] = k
			}
		}
		item_map[s] = item
	}
	mapping["config_schema"] = item_map

	return mapping, nil
}

func stringToInterface(s string) (interface{}, error) {
	var v interface{}

	// Try if the string is yaml
	err := yaml.Unmarshal([]byte(s), &v)
	if err != nil {
		// Try if the string is json
		err = json.Unmarshal([]byte(s), &v)
		if err != nil {
			return nil, fmt.Errorf("unable to parse string using yaml or json")
		}
	}
	return v, nil
}

func stringToMap(s string) ([]map[string]string, error) {
	r := csv.NewReader(strings.NewReader(s))
	rows := []map[string]string{}
	var header []string
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header == nil {
			header = record
		} else {
			dict := map[string]string{}
			for i := range header {
				dict[header[i]] = record[i]
			}
			rows = append(rows, dict)
		}
	}
	return rows, nil
}
