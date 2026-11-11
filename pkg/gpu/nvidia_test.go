package gpu

import (
	"encoding/xml"
	"testing"
)

func parseNvidiaGPU(t *testing.T, body string) GPU {
	t.Helper()
	var s NvidiaSMI
	doc := `<nvidia_smi_log><attached_gpus>1</attached_gpus><gpu id="0">` + body + `</gpu></nvidia_smi_log>`
	if err := xml.Unmarshal([]byte(doc), &s); err != nil {
		t.Fatal(err)
	}
	return s.GPUs[0]
}

func TestNvidiaPowerAndTemperature(t *testing.T) {
	cases := []struct {
		name            string
		body            string
		temp, draw, lim float64
	}{
		{
			name: "R550+ instant and average draw",
			body: `<temperature><gpu_temp>63 C</gpu_temp></temperature>
				<gpu_power_readings><power_state>P2</power_state>
				<average_power_draw>210.10 W</average_power_draw>
				<instant_power_draw>215.42 W</instant_power_draw>
				<current_power_limit>450.00 W</current_power_limit></gpu_power_readings>`,
			temp: 63, draw: 215.42, lim: 450,
		},
		{
			name: "R530 gpu_power_readings with power_draw",
			body: `<temperature><gpu_temp>41 C</gpu_temp></temperature>
				<gpu_power_readings><power_draw>30.12 W</power_draw>
				<current_power_limit>350.00 W</current_power_limit></gpu_power_readings>`,
			temp: 41, draw: 30.12, lim: 350,
		},
		{
			name: "older power_readings",
			body: `<temperature><gpu_temp>55 C</gpu_temp></temperature>
				<power_readings><power_draw>120.50 W</power_draw>
				<power_limit>250.00 W</power_limit></power_readings>`,
			temp: 55, draw: 120.5, lim: 250,
		},
		{
			name: "not reported",
			body: `<temperature><gpu_temp>N/A</gpu_temp></temperature>
				<gpu_power_readings><power_draw>N/A</power_draw></gpu_power_readings>`,
		},
	}
	for _, c := range cases {
		g := parseNvidiaGPU(t, c.body)
		draw, lim := g.power()
		temp := firstUnitValue(g.Temperature.GPUTemp)
		if temp != c.temp || draw != c.draw || lim != c.lim {
			t.Errorf("%s: got temp=%v draw=%v limit=%v, want %v %v %v", c.name, temp, draw, lim, c.temp, c.draw, c.lim)
		}
	}
}
