package cql2

import (
	"encoding/json"
	"math/big"
	"slices"
	"strings"
	"unicode"

	"github.com/mapseekai/ogc-api/common"
)

func (p *textParser) geometry(name string, depth int, z bool) Geometry {
	if !p.enter(depth) {
		return Geometry{}
	}
	g := Geometry{Type: geometryName(name)}
	switch g.Type {
	case "BBox":
		for p.err == nil {
			if p.token.kind != 'n' {
				p.fail("bbox ordinate")
				break
			}
			n, err := numberLiteral(p.token.text)
			if err != nil {
				p.err = err
				break
			}
			if len(g.Bounds) == 6 {
				p.fail("bbox ordinate count")
				break
			}
			g.Bounds = append(g.Bounds, n.Number)
			p.advance()
			if !p.take(',') {
				break
			}
		}
	case "GeometryCollection":
		for p.err == nil {
			child := p.operand(depth + 1)
			if child.Literal == nil || child.Literal.Geometry == nil {
				p.fail("collection geometry")
				break
			}
			g.Geometries = append(g.Geometries, *child.Literal.Geometry)
			if !p.take(',') {
				break
			}
		}
	default:
		if g.Type == "MultiPoint" && p.token.kind != '(' {
			p.fail("parenthesized multipoint positions")
		}
		g.Coordinates = p.coordinateBody(depth + 1)
	}
	p.expect(')')
	if z && geometryDimension(g) != 3 {
		p.fail("Z geometry dimension")
	}
	return g
}

func (p *textParser) coordinateBody(depth int) Coordinates {
	if !p.enter(depth) {
		return Coordinates{}
	}
	p.nodes++
	if p.nodes > p.limits.MaxNodes {
		p.fail("coordinate count")
		return Coordinates{}
	}
	var c Coordinates
	if p.token.kind == '(' {
		for p.take('(') && p.err == nil {
			c.Children = append(c.Children, p.coordinateBody(depth+1))
			p.expect(')')
			if !p.take(',') {
				break
			}
		}
		return c
	}
	var positions []Coordinates
	for p.err == nil {
		p.nodes++
		if p.nodes > p.limits.MaxNodes {
			p.fail("coordinate count")
			break
		}
		var position []json.Number
		end := -1
		for p.token.kind == 'n' && p.err == nil {
			if end >= 0 {
				gap := p.input[end:p.token.start]
				if gap == "" || strings.IndexFunc(gap, func(r rune) bool { return !unicode.IsSpace(r) }) >= 0 {
					p.fail("coordinate separator")
					break
				}
			}
			n, err := numberLiteral(p.token.text)
			if err != nil {
				p.err = err
				break
			}
			if len(position) == 3 {
				p.fail("position dimension")
				break
			}
			position = append(position, n.Number)
			end = p.token.start + len(p.token.text)
			p.advance()
		}
		if len(position) == 0 {
			p.fail("coordinate position")
			break
		}
		positions = append(positions, Coordinates{Position: position})
		if !p.take(',') {
			break
		}
	}
	if len(positions) == 1 {
		return positions[0]
	}
	return Coordinates{Children: positions}
}

func jsonGeometry(object map[string]any, limits Limits, depth int) (*Geometry, error) {
	if depth > limits.MaxDepth {
		return nil, invalid("geometry depth")
	}
	if bounds, ok := object["bbox"]; ok && len(object) == 1 {
		numbers, err := jsonNumbers(bounds)
		if err != nil {
			return nil, err
		}
		return &Geometry{Type: "BBox", Bounds: numbers}, nil
	}
	name, ok := object["type"].(string)
	if !ok || geometryName(name) != name || name == "BBox" {
		return nil, invalid("geometry type")
	}
	g := &Geometry{Type: name}
	for key := range object {
		if key != "type" && key != "coordinates" && key != "geometries" && key != "bbox" {
			return nil, invalid("geometry member")
		}
	}
	if bounds, ok := object["bbox"]; ok {
		var err error
		g.Envelope, err = jsonNumbers(bounds)
		if err != nil {
			return nil, err
		}
	}
	if name == "GeometryCollection" {
		if _, ok := object["coordinates"]; ok {
			return nil, invalid("collection coordinates")
		}
		members, ok := object["geometries"].([]any)
		if !ok || len(members) == 0 {
			return nil, invalid("collection members")
		}
		for _, member := range members {
			m, ok := member.(map[string]any)
			if !ok {
				return nil, invalid("collection member")
			}
			child, err := jsonGeometry(m, limits, depth+1)
			if err != nil {
				return nil, err
			}
			g.Geometries = append(g.Geometries, *child)
		}
	} else {
		if _, ok := object["geometries"]; ok {
			return nil, invalid("geometry members")
		}
		var err error
		g.Coordinates, err = jsonCoordinates(object["coordinates"], limits, depth+1)
		if err != nil {
			return nil, err
		}
	}
	return g, nil
}

func jsonCoordinates(value any, limits Limits, depth int) (Coordinates, error) {
	if depth > limits.MaxDepth {
		return Coordinates{}, invalid("coordinate depth")
	}
	values, ok := value.([]any)
	if !ok || len(values) == 0 {
		return Coordinates{}, invalid("coordinates")
	}
	var c Coordinates
	if _, ok := values[0].(json.Number); ok {
		numbers, err := jsonNumbers(value)
		return Coordinates{Position: numbers}, err
	}
	for _, value := range values {
		child, err := jsonCoordinates(value, limits, depth+1)
		if err != nil {
			return Coordinates{}, err
		}
		c.Children = append(c.Children, child)
	}
	return c, nil
}

func jsonNumbers(value any) ([]json.Number, error) {
	values, ok := value.([]any)
	if !ok {
		return nil, invalid("coordinate array")
	}
	numbers := make([]json.Number, len(values))
	for i, value := range values {
		n, ok := value.(json.Number)
		if !ok {
			return nil, invalid("coordinate number")
		}
		literal, err := numberLiteral(string(n))
		if err != nil {
			return nil, err
		}
		numbers[i] = literal.Number
	}
	return numbers, nil
}

func validateGeometry(g *Geometry, crs common.CRS, nodes *int, limits Limits, depth int) error {
	*nodes++
	if *nodes > limits.MaxNodes || depth > limits.MaxDepth {
		return invalid("geometry complexity")
	}
	dimension := geometryDimension(*g)
	if dimension != 2 && dimension != 3 {
		return invalid("geometry dimension")
	}
	if crs == (common.CRS{}) {
		uri := crs84
		if dimension == 3 {
			uri = crs84h
		}
		crs, _ = common.CRSByURI(uri)
	}
	if crs.Dimensions != dimension {
		return invalid("CRS dimension")
	}
	crs.URI = strings.Clone(crs.URI)
	g.CRS, g.CRSDefinition = crs.URI, crs
	if len(g.Envelope) != 0 {
		if len(g.Envelope) != dimension*2 {
			return invalid("geometry envelope dimension")
		}
		if err := validateBounds(g.Envelope, crs); err != nil {
			return err
		}
	}
	if g.Type == "BBox" {
		return validateBounds(g.Bounds, crs)
	}
	if g.Type == "GeometryCollection" {
		if len(g.Geometries) < 2 {
			return invalid("geometry collection member count")
		}
		for i := range g.Geometries {
			if g.Geometries[i].Type == "GeometryCollection" || g.Geometries[i].Type == "BBox" {
				return invalid("geometry collection member type")
			}
			if geometryDimension(g.Geometries[i]) != dimension {
				return invalid("collection dimensions")
			}
			if err := validateGeometry(&g.Geometries[i], crs, nodes, limits, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	levels := map[string]int{"Point": 0, "LineString": 1, "Polygon": 2, "MultiPoint": 1, "MultiLineString": 2, "MultiPolygon": 3}
	level, ok := levels[g.Type]
	if !ok {
		return invalid("geometry type")
	}
	if err := validateCoordinates(g.Coordinates, level, crs, nodes, limits, depth+1); err != nil {
		return err
	}
	switch g.Type {
	case "LineString":
		return validateLine(g.Coordinates, false)
	case "Polygon":
		return validatePolygon(g.Coordinates)
	case "MultiLineString":
		for _, line := range g.Coordinates.Children {
			if err := validateLine(line, false); err != nil {
				return err
			}
		}
	case "MultiPolygon":
		for _, polygon := range g.Coordinates.Children {
			if err := validatePolygon(polygon); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateCoordinates(c Coordinates, level int, crs common.CRS, nodes *int, limits Limits, depth int) error {
	*nodes++
	if *nodes > limits.MaxNodes || depth > limits.MaxDepth {
		return invalid("coordinate complexity")
	}
	if level == 0 {
		if len(c.Position) != crs.Dimensions || len(c.Children) != 0 {
			return invalid("coordinate dimension")
		}
		if crs.Geographic {
			longitude, latitude := 0, 1
			if crs.AxisOrder == common.YX {
				longitude, latitude = 1, 0
			}
			if !inRange(c.Position[longitude], -180, 180) || !inRange(c.Position[latitude], -90, 90) {
				return invalid("coordinate range")
			}
		}
		return nil
	}
	if len(c.Position) != 0 || len(c.Children) == 0 {
		return invalid("coordinate nesting")
	}
	for _, child := range c.Children {
		if err := validateCoordinates(child, level-1, crs, nodes, limits, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateLine(c Coordinates, ring bool) error {
	minimum := 2
	if ring {
		minimum = 4
	}
	if len(c.Children) < minimum {
		return invalid("line position count")
	}
	if ring && !slices.Equal(c.Children[0].Position, c.Children[len(c.Children)-1].Position) {
		return invalid("unclosed polygon ring")
	}
	return nil
}

func validatePolygon(c Coordinates) error {
	for _, ring := range c.Children {
		if err := validateLine(ring, true); err != nil {
			return err
		}
	}
	return nil
}

func validateBounds(bounds []json.Number, crs common.CRS) error {
	if len(bounds) != crs.Dimensions*2 {
		return invalid("bbox dimension")
	}
	dimension := len(bounds) / 2
	longitude, latitude := 0, 1
	if crs.AxisOrder == common.YX {
		longitude, latitude = 1, 0
	}
	if crs.Geographic {
		if !inRange(bounds[longitude], -180, 180) || !inRange(bounds[longitude+dimension], -180, 180) || !inRange(bounds[latitude], -90, 90) || !inRange(bounds[latitude+dimension], -90, 90) {
			return invalid("bbox range")
		}
	}
	for i := 0; i < dimension; i++ {
		if !(crs.Geographic && i == longitude) && compareNumbers(bounds[i], bounds[i+dimension]) > 0 {
			return invalid("bbox order")
		}
	}
	return nil
}

func geometryDimension(g Geometry) int {
	if g.Type == "BBox" {
		return len(g.Bounds) / 2
	}
	if g.Type == "GeometryCollection" {
		if len(g.Geometries) == 0 {
			return 0
		}
		return geometryDimension(g.Geometries[0])
	}
	c := g.Coordinates
	for len(c.Children) > 0 {
		c = c.Children[0]
	}
	return len(c.Position)
}

func geometryName(name string) string {
	for _, value := range []string{"Point", "LineString", "Polygon", "MultiPoint", "MultiLineString", "MultiPolygon", "GeometryCollection", "BBox"} {
		if strings.EqualFold(name, value) {
			return value
		}
	}
	return ""
}

func inRange(number json.Number, minimum, maximum int64) bool {
	n, ok := new(big.Rat).SetString(string(number))
	return ok && n.Cmp(big.NewRat(minimum, 1)) >= 0 && n.Cmp(big.NewRat(maximum, 1)) <= 0
}
func compareNumbers(a, b json.Number) int {
	x, _ := new(big.Rat).SetString(string(a))
	y, _ := new(big.Rat).SetString(string(b))
	return x.Cmp(y)
}
