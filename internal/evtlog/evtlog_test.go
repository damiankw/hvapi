package evtlog

import (
	"testing"
	"time"
)

const sample = `<Event xmlns="http://schemas.microsoft.com/win/2004/08/events/event"><System><Provider Name="Microsoft-Windows-Hyper-V-VMMS" Guid="{6066F867-...}"/><EventID>16010</EventID><Version>0</Version><Level>2</Level><Task>0</Task><Opcode>0</Opcode><Keywords>0x8000000000000000</Keywords><TimeCreated SystemTime="2026-10-20T08:15:30.123456700Z"/><EventRecordID>4211</EventRecordID><Correlation/><Execution ProcessID="1234" ThreadID="5678"/><Channel>Microsoft-Windows-Hyper-V-VMMS-Admin</Channel><Computer>HV01.example.local</Computer><Security UserID="S-1-5-18"/></System><UserData><VmlEventLog xmlns="http://www.microsoft.com/Windows/Virtualization/Events"><VmName>Web Server</VmName><VmId>11111111-1111-1111-1111-111111111111</VmId><ErrorCode>0x80070002</ErrorCode></VmlEventLog></UserData></Event>`

func TestParseXML(t *testing.T) {
	e, err := ParseXML(sample, "")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != 16010 || e.Level != "Error" || e.LevelCode != 2 || e.Provider != "Microsoft-Windows-Hyper-V-VMMS" || e.RecordID != 4211 ||
		!e.Time.Equal(time.Date(2026, 10, 20, 8, 15, 30, 123456700, time.UTC)) || e.Computer != "HV01.example.local" ||
		len(e.Data) != 3 || e.Data[0] != "Web Server" || e.VMID != "11111111-1111-1111-1111-111111111111" || e.Message != "Web Server 11111111-1111-1111-1111-111111111111 0x80070002" {
		t.Errorf("event = %+v", e)
	}
	e2, _ := ParseXML(sample, "The VM failed to start: file not found (0x80070002).")
	if e2.Message != "The VM failed to start: file not found (0x80070002)." {
		t.Errorf("message = %q", e2.Message)
	}
	withData := `<Event><System><Provider Name="P"/><EventID>2</EventID><Level>4</Level><TimeCreated SystemTime="2026-10-20T00:00:00Z"/><EventRecordID>1</EventRecordID><Channel>C</Channel></System><EventData><Data Name="SchedulerType">0x3</Data></EventData></Event>`
	e3, _ := ParseXML(withData, "")
	if e3.Level != "Information" || len(e3.Data) != 1 || e3.Data[0] != "0x3" || e3.VMID != "" {
		t.Errorf("event3 = %+v", e3)
	}
	if _, err := ParseXML("not xml", ""); err == nil {
		t.Error("garbage should fail")
	}
	if SinceQuery(24*time.Hour) != "*[System[TimeCreated[timediff(@SystemTime) <= 86400000]]]" {
		t.Error("SinceQuery")
	}
}
