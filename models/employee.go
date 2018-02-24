package models

import (
	"fmt"
	"github.com/astaxie/beego"
	_ "github.com/denisenkom/go-mssqldb"
)

const (
	CountEmployeeCmd = "SELECT count(*) FROM bd_org_emp WHERE tenant_layer_code = %d AND del_flag = 0;"
	QueryEmployeeCmd = "SELECT rfid FROM bd_org_emp WHERE tenant_layer_code = %d AND del_flag = 0;"
)

func LoadEmployeeRFIDByTenant(tenantLayerCode int64) ([]string, error) {
	conn := Dial()
	defer conn.Close()

	var count int
	countSql := fmt.Sprintf(CountEmployeeCmd, tenantLayerCode)
	err := conn.QueryRow(countSql).Scan(&count)

	querySql := fmt.Sprintf(QueryEmployeeCmd, tenantLayerCode)
	stmt, err := conn.Prepare(querySql)
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		beego.Error("Error query rfid by tenant", err)
		return nil, err
	}
	defer rows.Close()

	rfidList := make([]string, count)
	for rows.Next() {
		var rfid string
		rows.Scan(&rfid)
		rfidList = append(rfidList, rfid)
	}

	return rfidList, nil
}
