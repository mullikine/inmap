/*
Copyright © 2013 the InMAP authors.
This file is part of InMAP.

InMAP is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

InMAP is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with InMAP.  If not, see <http://www.gnu.org/licenses/>.
*/

package inmap

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ctessum/aep"
	"github.com/ctessum/geom"
	"github.com/ctessum/geom/encoding/shp"
	"github.com/ctessum/geom/proj"
	"github.com/ctessum/unit"
)

const (
	TestEmisFilename   = "testEmis.shp"
	TestOutputFilename = "testOutput.shp"
)

func WriteTestEmis() error {
	type emisHolder struct {
		geom.Polygon
		VOC, NOx, NH3, SOx float64 // emissions [tons/year]
		PM25               float64 `shp:"PM2_5"` // emissions [tons/year]
		Height             float64 // stack height [m]
		Diam               float64 // stack diameter [m]
		Temp               float64 // stack temperature [K]
		Velocity           float64 // stack velocity [m/s]
	}

	const (
		massConv = 907184740000.       // μg per short ton
		timeConv = 3600. * 8760.       // seconds per year
		emisConv = massConv / timeConv // convert tons/year to μg/s
		ETons    = E / emisConv        // emissions in tons per year
	)

	eShp, err := shp.NewEncoder(TestEmisFilename, emisHolder{})
	if err != nil {
		return err
	}

	emis := []emisHolder{
		{
			Polygon: geom.Polygon{{
				geom.Point{X: -3999, Y: -3999},
				geom.Point{X: -3001, Y: -3001},
				geom.Point{X: -3001, Y: -3999},
			}},
			VOC:  ETons,
			NOx:  ETons,
			NH3:  ETons,
			SOx:  ETons,
			PM25: ETons,
		},
		{
			Polygon: geom.Polygon{{
				geom.Point{X: -3999, Y: -3999},
				geom.Point{X: -3001, Y: -3001},
				geom.Point{X: -3001, Y: -3999},
			}},
			PM25:   ETons,
			Height: 20, // Layer 0
		},
		{
			Polygon: geom.Polygon{{
				geom.Point{X: -3999, Y: -3999},
				geom.Point{X: -3001, Y: -3001},
				geom.Point{X: -3001, Y: -3999},
			}},
			PM25:   ETons,
			Height: 150, // Layer 2
		},
		{
			Polygon: geom.Polygon{{
				geom.Point{X: -3999, Y: -3999},
				geom.Point{X: -3001, Y: -3001},
				geom.Point{X: -3001, Y: -3999},
			}},
			PM25:   ETons,
			Height: 2000, // Layer 9
		},
		{
			Polygon: geom.Polygon{{
				geom.Point{X: -3999, Y: -3999},
				geom.Point{X: -3001, Y: -3001},
				geom.Point{X: -3001, Y: -3999},
			}},
			PM25:   ETons,
			Height: 3000, // Above layer 9
		},
	}

	for _, e := range emis {
		if err = eShp.Encode(e); err != nil {
			return err
		}
	}
	eShp.Close()

	f, err := os.Create(strings.TrimSuffix(TestEmisFilename, filepath.Ext(TestEmisFilename)) + ".prj")
	if err != nil {
		panic(err)
	}
	if _, err = f.Write([]byte(TestGridSR)); err != nil {
		panic(err)
	}
	f.Close()

	return nil
}

func TestEmissions(t *testing.T) {
	const tol = 1.e-8 // test tolerance

	if err := WriteTestEmis(); err != nil {
		t.Error(err)
		t.FailNow()
	}
	sr, err := proj.Parse(TestGridSR)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	emis, err := ReadEmissionShapefiles(sr, "tons/year", nil, TestEmisFilename)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	cfg, ctmdata, pop, popIndices, mr, mortIndices := VarGridData()

	d := &InMAP{
		InitFuncs: []DomainManipulator{
			cfg.RegularGrid(ctmdata, pop, popIndices, mr, mortIndices, emis),
		},
	}
	if err := d.Init(); err != nil {
		t.Error(err)
	}

	type test struct {
		cellIndex int
		polIndex  []int
		values    []float64
	}
	var tests = []test{
		{
			cellIndex: 0,
			polIndex:  []int{igOrg, igS, igNH, igNO, iPM2_5},
			values:    []float64{E, E * SOxToS, E * NH3ToN, E * NOxToN, E * 2},
		},
		{
			cellIndex: 2 * 4, // layer 2, 4 cells per layer
			polIndex:  []int{iPM2_5},
			values:    []float64{E},
		},
		{
			cellIndex: 9 * 4, // layer 9, 4 cells per layer
			polIndex:  []int{iPM2_5},
			values:    []float64{E * 2},
		},
	}

	cells := d.cells.array()
	nonzero := make(map[int]map[int]int)
	for _, tt := range tests {
		c := cells[tt.cellIndex]
		nonzero[tt.cellIndex] = make(map[int]int)
		for i, ii := range tt.polIndex {
			nonzero[tt.cellIndex][ii] = 0
			if different(c.EmisFlux[ii]*c.Volume, tt.values[i], tol) {
				t.Errorf("emissions value for cell %d pollutant %d should be %g but is %g",
					tt.cellIndex, ii, tt.values[i], c.EmisFlux[ii]*c.Volume)
			}
		}
	}
	for i, c := range cells {
		for ii, e := range c.EmisFlux {
			if _, ok := nonzero[i][ii]; !ok {
				if e != 0 {
					t.Errorf("emissions for cell %d pollutant %d should be zero but is %g",
						i, ii, e*c.Volume)
				}
			}
		}
	}
	DeleteShapefile(TestEmisFilename)
}

func TestOutputEquation(t *testing.T) {
	cfg, ctmdata, pop, popIndices, mr, mortIndices := VarGridData()

	emis := NewEmissions()
	emis.Add(&EmisRecord{
		PM25: E,
		Geom: geom.Point{X: -3999, Y: -3999.},
	}) // ground level emissions

	o, err := NewOutputter(TestOutputFilename, false, map[string]string{
		"WindSpeed":  "WindSpeed",
		"DoubleWind": "WindSpeed * 2",
		"ExpWind":    "exp(WindSpeed)",
		"ExpTwoWind": "exp(DoubleWind)"},
		nil)
	if err != nil {
		t.Error(err)
	}

	d := &InMAP{
		InitFuncs: []DomainManipulator{
			cfg.RegularGrid(ctmdata, pop, popIndices, mr, mortIndices, emis),
		},
		CleanupFuncs: []DomainManipulator{
			o.Output(),
		},
	}
	if err = d.Init(); err != nil {
		t.Error(err)
	}
	if err = d.Cleanup(); err != nil {
		t.Error(err)
	}
	type outData struct {
		WindSpeed  float64
		DoubleWind float64
		ExpWind    float64
		ExpTwoWind float64
	}
	dec, err := shp.NewDecoder(TestOutputFilename)
	if err != nil {
		t.Fatal(err)
	}
	var recs []outData
	for {
		var rec outData
		if more := dec.DecodeRow(&rec); !more {
			break
		}
		recs = append(recs, rec)
	}
	if err := dec.Error(); err != nil {
		t.Fatal(err)
	}

	want := []outData{
		{
			WindSpeed:  2.16334701,
			DoubleWind: 4.32669401,
			ExpWind:    8.70020863,
			ExpTwoWind: 75.69363021,
		},
		{
			WindSpeed:  1.88434911,
			DoubleWind: 1.88434911 * 2,
			ExpWind:    6.58206883,
			ExpTwoWind: 43.32363008,
		},
		{
			WindSpeed:  2.7272017,
			DoubleWind: 2.7272017 * 2,
			ExpWind:    15.29004098,
			ExpTwoWind: 233.78535321,
		},
		{
			WindSpeed:  2.56135321,
			DoubleWind: 5.12270641,
			ExpWind:    12.953334,
			ExpTwoWind: 167.78886168,
		},
	}

	if len(recs) != len(want) {
		t.Errorf("want %d records but have %d", len(want), len(recs))
	}
	for i, w := range want {
		if i >= len(recs) {
			continue
		}
		h := recs[i]
		if !reflect.DeepEqual(w, h) {
			t.Errorf("record %d: want %+v but have %+v", i, w, h)
		}
	}
	dec.Close()
	DeleteShapefile(TestOutputFilename)
}

func BenchmarkOutput(b *testing.B) {
	cfg, ctmdata, pop, popIndices, mr, mortIndices := VarGridData()

	emis := NewEmissions()
	emis.Add(&EmisRecord{
		PM25: E,
		Geom: geom.Point{X: -3999, Y: -3999.},
	}) // ground level emissions

	var o *Outputter

	b.Run("NewOutputter", func(b *testing.B) {
		oBench, err := NewOutputter(TestOutputFilename, false, map[string]string{
			"TotalPop":   "TotalPop",
			"WhiteNoLat": "WhiteNoLat",
			"NPctWNoLat": "{sum(WhiteNoLat) / sum(TotalPop)}",
			"NPctOther":  "{(sum(TotalPop) - sum(WhiteNoLat)) / sum(TotalPop)}",
			"NPctRatio":  "NPctWNoLat / NPctOther",
			"TotalPopD":  "coxHazard(loglogRR(TotalPM25), TotalPop, MortalityRate)",
			"TotalPM25":  "TotalPM25",
			"PM25Emiss":  "PM25Emissions",
			"BasePM25":   "BaselineTotalPM25",
			"WindSpeed":  "WindSpeed"},
			nil)
		if err != nil {
			b.Error(err)
		}
		o = oBench
	})

	d := &InMAP{
		InitFuncs: []DomainManipulator{
			cfg.RegularGrid(ctmdata, pop, popIndices, mr, mortIndices, emis),
			o.CheckOutputVars(),
		},
		CleanupFuncs: []DomainManipulator{
			o.Output(),
		},
	}
	b.Run("InitFuncs", func(b *testing.B) {
		if err := d.Init(); err != nil {
			b.Error(err)
		}
	})
	b.Run("CleanupFuncs", func(b *testing.B) {
		if err := d.Cleanup(); err != nil {
			b.Error(err)
		}
	})
}

func TestOutput(t *testing.T) {
	cfg, ctmdata, pop, popIndices, mr, mortIndices := VarGridData()

	emis := NewEmissions()
	emis.Add(&EmisRecord{
		PM25: E,
		Geom: geom.Point{X: -3999, Y: -3999.},
	}) // ground level emissions

	o, err := NewOutputter(TestOutputFilename, false, map[string]string{
		"TotalPop":   "TotalPop",
		"WhiteNoLat": "WhiteNoLat",
		"NPctWNoLat": "{sum(WhiteNoLat) / sum(TotalPop)}",
		"NPctOther":  "{(sum(TotalPop) - sum(WhiteNoLat)) / sum(TotalPop)}",
		"NPctRatio":  "NPctWNoLat / NPctOther",
		"TotalPopD":  "coxHazard(loglogRR(TotalPM25), TotalPop, AllMort)",
		"TotalPM25":  "TotalPM25",
		"PM25Emiss":  "PM25Emissions",
		"BasePM25":   "BaselineTotalPM25",
		"WindSpeed":  "WindSpeed"},
		nil)
	if err != nil {
		t.Error(err)
	}

	d := &InMAP{
		InitFuncs: []DomainManipulator{
			cfg.RegularGrid(ctmdata, pop, popIndices, mr, mortIndices, emis),
			o.CheckOutputVars(),
		},
		CleanupFuncs: []DomainManipulator{
			o.Output(),
		},
	}
	if err := d.Init(); err != nil {
		t.Error(err)
	}
	if err := d.Cleanup(); err != nil {
		t.Error(err)
	}
	type outData struct {
		BaselineTotalPM25 float64 `shp:"BasePM25"`
		PM25Emissions     float64 `shp:"PM25Emiss"`
		TotalPM25         float64
		TotalPop          float64
		WhiteNoLat        float64
		NPctWNoLat        float64
		NPctOther         float64
		NPctRatio         float64
		Deaths            float64 `shp:"TotalPopD"`
		WindSpeed         float64
	}
	dec, err := shp.NewDecoder(TestOutputFilename)
	if err != nil {
		t.Fatal(err)
	}
	var recs []outData
	for {
		var rec outData
		if more := dec.DecodeRow(&rec); !more {
			break
		}
		recs = append(recs, rec)
	}
	if err := dec.Error(); err != nil {
		t.Fatal(err)
	}

	want := []outData{
		{
			BaselineTotalPM25: 4.90770054,
			PM25Emissions:     0.00112376, //E / d.Cells[0].Volume,
			TotalPop:          100000.,
			WhiteNoLat:        50000.,
			NPctWNoLat:        0.5,
			NPctOther:         0.5,
			NPctRatio:         1,
			WindSpeed:         2.16334701,
		},
		{
			BaselineTotalPM25: 10.34742928,
			WindSpeed:         1.88434911,
			NPctWNoLat:        0.5,
			NPctOther:         0.5,
			NPctRatio:         1,
		},
		{
			BaselineTotalPM25: 4.2574172,
			WindSpeed:         2.7272017,
			NPctWNoLat:        0.5,
			NPctOther:         0.5,
			NPctRatio:         1,
		},
		{
			BaselineTotalPM25: 5.36232233,
			WindSpeed:         2.56135321,
			NPctWNoLat:        0.5,
			NPctOther:         0.5,
			NPctRatio:         1,
		},
	}

	if len(recs) != len(want) {
		t.Errorf("want %d records but have %d", len(want), len(recs))
	}
	for i, w := range want {
		if i >= len(recs) {
			continue
		}
		h := recs[i]
		if !reflect.DeepEqual(w, h) {
			t.Errorf("record %d: want %+v but have %+v", i, w, h)
		}
	}
	dec.Close()
	DeleteShapefile(TestOutputFilename)
}

func TestRegrid(t *testing.T) {
	oldGeom := []geom.Polygonal{
		geom.Polygon{{
			geom.Point{X: -1, Y: -1},
			geom.Point{X: 1, Y: -1},
			geom.Point{X: 1, Y: 1},
			geom.Point{X: -1, Y: 1},
		}},
	}
	newGeom := []geom.Polygonal{
		geom.Polygon{{
			geom.Point{X: -2, Y: -2},
			geom.Point{X: 0, Y: -2},
			geom.Point{X: 0, Y: 0},
			geom.Point{X: -2, Y: 0},
		}},
		geom.Polygon{{
			geom.Point{X: 0, Y: -2},
			geom.Point{X: 2, Y: -2},
			geom.Point{X: 2, Y: 0},
			geom.Point{X: 0, Y: 0},
		}},
		geom.Polygon{{
			geom.Point{X: 0, Y: 0},
			geom.Point{X: 4, Y: 0},
			geom.Point{X: 4, Y: 4},
			geom.Point{X: 0, Y: 4},
		}},
		geom.Polygon{{
			geom.Point{X: -1, Y: 0},
			geom.Point{X: 0, Y: 0},
			geom.Point{X: 0, Y: 1},
			geom.Point{X: -1, Y: 1},
		}},
	}
	oldData := []float64{1.}
	newData, err := Regrid(oldGeom, newGeom, oldData)
	if err != nil {
		t.Error(err)
	}
	want := []float64{0.25, 0.25, 0.0625, 1}
	if !reflect.DeepEqual(newData, want) {
		t.Errorf("have %v, want %v", newData, want)
	}
}

func TestCellIntersections(t *testing.T) {
	cfg, ctmdata, pop, popIndices, mr, mortIndices := VarGridData()

	emis := NewEmissions()

	mutator, err := PopulationMutator(cfg, popIndices)
	if err != nil {
		t.Error(err)
	}
	d := &InMAP{
		InitFuncs: []DomainManipulator{
			cfg.RegularGrid(ctmdata, pop, popIndices, mr, mortIndices, emis),
			cfg.MutateGrid(mutator, ctmdata, pop, mr, emis, nil),
		},
	}
	if err := d.Init(); err != nil {
		t.Error(err)
	}

	cells, fractions := d.CellIntersections(geom.Point{X: 0, Y: -2000})

	sort.Sort(&cellsFracSorter{
		cellsSorter: cellsSorter{
			cells: cells,
		},
		fractions: fractions,
	})

	type answer struct {
		b     *geom.Bounds
		layer int
		frac  float64
	}
	expected := []answer{
		{
			b:     &geom.Bounds{Min: geom.Point{X: -2000, Y: -4000}, Max: geom.Point{X: 0, Y: -2000}},
			layer: 0,
			frac:  0.25,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -2000, Y: -2000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 0,
			frac:  0.25,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 0,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -4000, Y: -4000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 1,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 1,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -4000, Y: -4000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 2,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 2,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -4000, Y: -4000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 3,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 3,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -4000, Y: -4000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 4,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 4,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -4000, Y: -4000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 5,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 5,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -4000, Y: -4000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 6,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 6,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -4000, Y: -4000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 7,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 7,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -4000, Y: -4000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 8,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 8,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: -4000, Y: -4000}, Max: geom.Point{X: 0, Y: 0}},
			layer: 9,
			frac:  0.5,
		},
		{
			b:     &geom.Bounds{Min: geom.Point{X: 0, Y: -4000}, Max: geom.Point{X: 4000, Y: 0}},
			layer: 9,
			frac:  0.5,
		},
	}

	if len(cells) != len(expected) {
		t.Fatalf("wrong number of cells: %d != %d", len(cells), len(expected))
	}

	for i, cell := range cells {
		if !reflect.DeepEqual(cell.Bounds(), expected[i].b) {
			t.Errorf("%d: bounds don't match: have %v but want %v", i, cell.Bounds(), expected[i].b)
		}
		if cell.Layer != expected[i].layer {
			t.Errorf("%d: layers don't match: have %d but want %d", i, cell.Layer, expected[i].layer)
		}
		if fractions[i] != expected[i].frac {
			t.Errorf("%d: fractions don't have match: %g but want %g", i, fractions[i], expected[i].frac)
		}
	}
}

// sortCells sorts the cells by layer, x centroid, and y centroid.
func sortCells(cells []*Cell) {
	sc := &cellsSorter{
		cells: cells,
	}
	sort.Sort(sc)
}

type cellsSorter struct {
	cells []*Cell
}

// Len is part of sort.Interface.
func (c *cellsSorter) Len() int {
	return len(c.cells)
}

// Swap is part of sort.Interface.
func (c *cellsSorter) Swap(i, j int) {
	c.cells[i], c.cells[j] = c.cells[j], c.cells[i]
}

func (c *cellsSorter) Less(i, j int) bool {
	ci := c.cells[i]
	cj := c.cells[j]
	if ci.Layer != cj.Layer {
		return ci.Layer < cj.Layer
	}

	icent := ci.Polygonal.Centroid()
	jcent := cj.Polygonal.Centroid()

	if icent.X != jcent.X {
		return icent.X < jcent.X
	}
	if icent.Y != jcent.Y {
		return icent.Y < jcent.Y
	}
	fmt.Printf("%#v\n", ci.Polygonal)
	fmt.Printf("%#v\n", cj.Polygonal)
	fmt.Println(ci.Layer, cj.Layer, icent.X, jcent.X, icent.Y, jcent.Y)
	// We apparently have concentric or identical cells if we get to here.
	panic(fmt.Errorf("problem sorting: i: %v, j: %v", ci, cj))
}

type cellsFracSorter struct {
	cellsSorter
	fractions []float64
}

func (c *cellsFracSorter) Swap(i, j int) {
	c.cells[i], c.cells[j] = c.cells[j], c.cells[i]
	c.fractions[i], c.fractions[j] = c.fractions[j], c.fractions[i]
}

func TestFromAEP(t *testing.T) {
	cfg, ctmdata, pop, popIndices, mr, mortIndices := VarGridData()

	d := &InMAP{
		InitFuncs: []DomainManipulator{
			cfg.RegularGrid(ctmdata, pop, popIndices, mr, mortIndices, nil),
		},
	}
	if err := d.Init(); err != nil {
		t.Error(err)
	}
	grid := d.GetGeometry(0, false)
	gridSR, err := proj.Parse(cfg.GridProj)
	if err != nil {
		t.Fatal(err)
	}
	gridDef, err := aep.NewGridIrregular("inmap", grid, gridSR, gridSR)
	if err != nil {
		t.Fatal(err)
	}
	inputSR, err := proj.Parse("+proj=longlat")
	if err != nil {
		t.Fatal(err)
	}

	sp := aep.NewSpatialProcessor(aep.NewSrgSpecs(), []*aep.GridDef{gridDef}, &aep.GridRef{}, inputSR, false)

	e1 := new(aep.Emissions)
	begin, _ := time.Parse("Jan 2006", "Jan 2005")
	end, _ := time.Parse("Jan 2006", "Jan 2006")
	rate := unit.New(1, map[unit.Dimension]int{unit.MassDim: 1, unit.TimeDim: -1})
	e1.Add(begin, end, "pm25", "", rate)
	r1 := &aep.PointRecord{
		PointSourceData: aep.PointSourceData{
			StackHeight:   unit.New(1, unit.Meter),
			StackVelocity: unit.New(1, unit.MeterPerSecond),
			StackDiameter: unit.New(1, unit.Meter),
			StackTemp:     unit.New(1, unit.Kelvin),
			Point:         geom.Point{X: -96.99, Y: 39.99},
			SR:            inputSR,
		},
		Emissions: *e1,
	}
	r2 := &aep.PointRecord{
		PointSourceData: aep.PointSourceData{
			StackHeight:   unit.New(0, unit.Meter),
			StackVelocity: unit.New(0, unit.MeterPerSecond),
			Point:         geom.Point{X: -97.01, Y: 40.01},
			SR:            inputSR,
		},
		Emissions: *e1,
	}

	const kgPerSecondToUgPerSecond = 1.0e9

	tests := []struct {
		name   string
		recs   []aep.Record
		result []*EmisRecord
	}{
		{
			name: "r1",
			recs: []aep.Record{r1},
			result: []*EmisRecord{
				&EmisRecord{
					Geom:     geom.Point{X: 2000, Y: -2000},
					PM25:     kgPerSecondToUgPerSecond,
					Height:   1,
					Diam:     1,
					Temp:     1,
					Velocity: 1,
				},
			},
		},
		{
			name: "r2",
			recs: []aep.Record{r2},
			result: []*EmisRecord{
				&EmisRecord{
					Geom: geom.Point{X: -2000, Y: 2000},
					PM25: kgPerSecondToUgPerSecond,
				},
			},
		},
		{
			name: "r1, r2",
			recs: []aep.Record{r1, r2},
			result: []*EmisRecord{
				&EmisRecord{
					Geom:     geom.Point{X: 2000, Y: -2000},
					PM25:     kgPerSecondToUgPerSecond,
					Height:   1,
					Diam:     1,
					Temp:     1,
					Velocity: 1,
				},
				&EmisRecord{
					Geom: geom.Point{X: -2000, Y: 2000},
					PM25: kgPerSecondToUgPerSecond,
				},
			},
		},
		{
			name: "r1, r1, r1",
			recs: []aep.Record{r1, r1, r1},
			result: []*EmisRecord{
				&EmisRecord{
					Geom:     geom.Point{X: 2000, Y: -2000},
					PM25:     kgPerSecondToUgPerSecond,
					Height:   1,
					Diam:     1,
					Temp:     1,
					Velocity: 1,
				},
				&EmisRecord{
					Geom:     geom.Point{X: 2000, Y: -2000},
					PM25:     kgPerSecondToUgPerSecond,
					Height:   1,
					Diam:     1,
					Temp:     1,
					Velocity: 1,
				},
				&EmisRecord{
					Geom:     geom.Point{X: 2000, Y: -2000},
					PM25:     kgPerSecondToUgPerSecond,
					Height:   1,
					Diam:     1,
					Temp:     1,
					Velocity: 1,
				},
			},
		},
		{
			name: "r2, r2, r2",
			recs: []aep.Record{r2, r2, r2},
			result: []*EmisRecord{
				&EmisRecord{
					Geom: geom.Point{X: -2000, Y: 2000},
					PM25: 3 * kgPerSecondToUgPerSecond,
				},
			},
		},
		{
			name: "r1, r2, r1",
			recs: []aep.Record{r1, r2, r1},
			result: []*EmisRecord{
				&EmisRecord{
					Geom:     geom.Point{X: 2000, Y: -2000},
					PM25:     kgPerSecondToUgPerSecond,
					Height:   1,
					Diam:     1,
					Temp:     1,
					Velocity: 1,
				},
				&EmisRecord{
					Geom:     geom.Point{X: 2000, Y: -2000},
					PM25:     kgPerSecondToUgPerSecond,
					Height:   1,
					Diam:     1,
					Temp:     1,
					Velocity: 1,
				},
				&EmisRecord{
					Geom: geom.Point{X: -2000, Y: 2000},
					PM25: kgPerSecondToUgPerSecond,
				},
			},
		},
		{
			name: "r2, r1, r2",
			recs: []aep.Record{r2, r1, r2},
			result: []*EmisRecord{
				&EmisRecord{
					Geom:     geom.Point{X: 2000, Y: -2000},
					PM25:     kgPerSecondToUgPerSecond,
					Height:   1,
					Diam:     1,
					Temp:     1,
					Velocity: 1,
				},
				&EmisRecord{
					Geom: geom.Point{X: -2000, Y: 2000},
					PM25: 2 * kgPerSecondToUgPerSecond,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			er, err := FromAEP(test.recs, sp, 0,
				[]aep.Pollutant{{Name: "voc"}},
				[]aep.Pollutant{{Name: "nox"}},
				[]aep.Pollutant{{Name: "nh3"}},
				[]aep.Pollutant{{Name: "sox"}},
				[]aep.Pollutant{{Name: "pm25"}},
			)
			if err != nil {
				t.Error(err)
			}
			if len(er) != len(test.result) {
				t.Fatalf("length: want %d but have %d", len(test.result), len(er))
			}
			for i, have := range er {
				want := test.result[i]
				if !reflect.DeepEqual(want, have) {
					t.Errorf("want %v but have %v", want, have)
				}
			}
		})
	}
}

func BenchmarkFromAEP(b *testing.B) {
	cfg, ctmdata, pop, popIndices, mr, mortIndices := VarGridData()

	d := &InMAP{
		InitFuncs: []DomainManipulator{
			cfg.RegularGrid(ctmdata, pop, popIndices, mr, mortIndices, nil),
		},
	}
	if err := d.Init(); err != nil {
		b.Error(err)
	}
	grid := d.GetGeometry(0, false)
	gridSR, err := proj.Parse(cfg.GridProj)
	if err != nil {
		b.Fatal(err)
	}
	gridDef, err := aep.NewGridIrregular("inmap", grid, gridSR, gridSR)
	if err != nil {
		b.Fatal(err)
	}
	inputSR, err := proj.Parse("+proj=longlat")
	if err != nil {
		b.Fatal(err)
	}

	sp := aep.NewSpatialProcessor(aep.NewSrgSpecs(), []*aep.GridDef{gridDef}, &aep.GridRef{}, inputSR, false)

	e1 := new(aep.Emissions)
	begin, _ := time.Parse("Jan 2006", "Jan 2005")
	end, _ := time.Parse("Jan 2006", "Jan 2006")
	rate := unit.New(1, map[unit.Dimension]int{unit.MassDim: 1, unit.TimeDim: -1})
	e1.Add(begin, end, "pm25", "", rate)
	r1 := &aep.PointRecord{
		PointSourceData: aep.PointSourceData{
			StackHeight:   unit.New(1, unit.Meter),
			StackVelocity: unit.New(1, unit.MeterPerSecond),
			StackDiameter: unit.New(1, unit.Meter),
			StackTemp:     unit.New(1, unit.Kelvin),
			Point:         geom.Point{X: -96.99, Y: 39.99},
			SR:            inputSR,
		},
		Emissions: *e1,
	}
	r2 := &aep.PointRecord{
		PointSourceData: aep.PointSourceData{
			StackHeight:   unit.New(0, unit.Meter),
			StackVelocity: unit.New(0, unit.MeterPerSecond),
			Point:         geom.Point{X: -97.01, Y: 40.01},
			SR:            inputSR,
		},
		Emissions: *e1,
	}

	const kgPerSecondToUgPerSecond = 1.0e9

	recs := []aep.Record{r1, r2}
	name := []string{"elevated", "ground level"}

	resultFuncs := []func(int) []*EmisRecord{
		func(n int) []*EmisRecord { // elevated emissions
			r := &EmisRecord{
				Geom:     geom.Point{X: 2000, Y: -2000},
				PM25:     kgPerSecondToUgPerSecond,
				Height:   1,
				Diam:     1,
				Temp:     1,
				Velocity: 1,
			}
			o := make([]*EmisRecord, n)
			for i := 0; i < n; i++ {
				o[i] = r
			}
			return o
		},
		func(n int) []*EmisRecord {
			return []*EmisRecord{
				&EmisRecord{
					Geom: geom.Point{X: -2000, Y: 2000},
					PM25: float64(n) * kgPerSecondToUgPerSecond,
				},
			}
		},
	}

	for i, rec := range recs {
		for _, n := range []int{10, 1000, 10000, 100000, 1000000} {
			r := make([]aep.Record, n)
			for j := 0; j < n; j++ {
				r[j] = rec
			}
			result := resultFuncs[i](n)
			b.Run(fmt.Sprintf("%s %d", name[i], n), func(b *testing.B) {
				er, err := FromAEP(r, sp, 0,
					[]aep.Pollutant{{Name: "voc"}},
					[]aep.Pollutant{{Name: "nox"}},
					[]aep.Pollutant{{Name: "nh3"}},
					[]aep.Pollutant{{Name: "sox"}},
					[]aep.Pollutant{{Name: "pm25"}},
				)
				if err != nil {
					b.Error(err)
				}
				if len(er) != len(result) {
					b.Fatalf("length: want %d but have %d", len(result), len(er))
				}
				for i, have := range er {
					want := result[i]
					if !reflect.DeepEqual(want, have) {
						b.Errorf("want %v but have %v", want, have)
					}
				}
			})
		}
	}
}
