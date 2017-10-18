package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/goxjs/gl"
	"github.com/goxjs/gl/glutil"
	"github.com/shurcooL/eX0/eX0-go/packet"
	"golang.org/x/mobile/exp/f32"
)

func newCharacter() (*character, error) {
	c := new(character)

	err := c.initShaders()
	if err != nil {
		return nil, err
	}
	err = c.createVbo()
	if err != nil {
		return nil, err
	}

	return c, nil
}

type character struct {
	vertexCount int

	program                 gl.Program
	pMatrixUniform          gl.Uniform
	mvMatrixUniform         gl.Uniform
	colorUniform            gl.Uniform
	vertexPositionBuffer    gl.Buffer
	vertexPositionAttribute gl.Attrib
}

func (l *character) initShaders() error {
	const (
		vertexSource = `//#version 120 // OpenGL 2.1.
//#version 100 // WebGL.

attribute vec3 aVertexPosition;

uniform mat4 uMVMatrix;
uniform mat4 uPMatrix;

void main() {
	gl_Position = uPMatrix * uMVMatrix * vec4(aVertexPosition, 1.0);
}
`
		fragmentSource = `//#version 120 // OpenGL 2.1.
//#version 100 // WebGL.

#ifdef GL_ES
	precision lowp float;
#endif

uniform vec3 uColor;

void main() {
	gl_FragColor = vec4(uColor, 1.0);
}
`
	)

	var err error
	l.program, err = glutil.CreateProgram(vertexSource, fragmentSource)
	if err != nil {
		return err
	}

	gl.ValidateProgram(l.program)
	if gl.GetProgrami(l.program, gl.VALIDATE_STATUS) != gl.TRUE {
		return errors.New("VALIDATE_STATUS: " + gl.GetProgramInfoLog(l.program))
	}

	gl.UseProgram(l.program)

	l.pMatrixUniform = gl.GetUniformLocation(l.program, "uPMatrix")
	l.mvMatrixUniform = gl.GetUniformLocation(l.program, "uMVMatrix")
	l.colorUniform = gl.GetUniformLocation(l.program, "uColor")

	if glError := gl.GetError(); glError != 0 {
		return fmt.Errorf("gl.GetError: %v", glError)
	}

	return nil
}

func (l *character) createVbo() error {
	l.vertexPositionBuffer = gl.CreateBuffer()
	gl.BindBuffer(gl.ARRAY_BUFFER, l.vertexPositionBuffer)

	var vertices []float32 = drawCircleBorderCustom(mgl32.Vec2{}, mgl32.Vec2{16, 16}, 2, 12, 1, 11)
	l.vertexCount = len(vertices) / 2

	vertices = append(vertices, -1, (3 + 10))
	vertices = append(vertices, -1, (3 - 1))
	vertices = append(vertices, 1, (3 - 1))
	vertices = append(vertices, 1, (3 + 10))

	gl.BufferData(gl.ARRAY_BUFFER, f32.Bytes(binary.LittleEndian, vertices...), gl.STATIC_DRAW)

	l.vertexPositionAttribute = gl.GetAttribLocation(l.program, "aVertexPosition")
	gl.EnableVertexAttribArray(l.vertexPositionAttribute)

	if glError := gl.GetError(); glError != 0 {
		return fmt.Errorf("gl.GetError: %v", glError)
	}

	return nil
}

func (l *character) setup() {
	gl.UseProgram(l.program)
	gl.BindBuffer(gl.ARRAY_BUFFER, l.vertexPositionBuffer)

	gl.VertexAttribPointer(l.vertexPositionAttribute, 2, gl.FLOAT, false, 0, 0)
}

func (l *character) render(team packet.Team) {
	switch team {
	case packet.Red:
		gl.Uniform3f(l.colorUniform, 1, 0, 0)
	case packet.Blue:
		gl.Uniform3f(l.colorUniform, 0, 0, 1)
	}

	first := 0
	count := l.vertexCount
	gl.DrawArrays(gl.TRIANGLE_STRIP, first, count)
	first += count
	count = 4
	gl.DrawArrays(gl.TRIANGLE_FAN, first, count)
}

// ---

func drawCircleBorderCustom(pos mgl32.Vec2, size mgl32.Vec2, borderWidth float32, totalSlices, startSlice, endSlice int32) (vertices []float32) {
	var x = float64(totalSlices)
	for i := startSlice; i <= endSlice; i++ {
		vertices = append(vertices, pos[0]+float32(math.Sin(Tau*float64(i)/x))*size[0]/2, pos[1]+float32(math.Cos(Tau*float64(i)/x))*size[1]/2)
		vertices = append(vertices, pos[0]+float32(math.Sin(Tau*float64(i)/x))*(size[0]/2-borderWidth), pos[1]+float32(math.Cos(Tau*float64(i)/x))*(size[1]/2-borderWidth))
	}
	return vertices
}
