package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"sync"
)

const (
	channels       = 2 // stereo
	bytesPerSample = 2
)

type Mixer interface {
	// Registers a mixing layer
	Register(id uint8, layer *MixingLayer) error
	// Unregisters a mixing layer
	Unregister(id uint8)
	// NextFrames mixes the next audio frames at discression of implementation (one sample per channel each).
	NextFrames() ([]byte, error)
	// Closes all layers on the mixer
	CloseLayers()
}

func NewGeneralMixer(frameCount int) Mixer {
	return &generalMixer{
		frameCount: frameCount,
		layers:     make(map[uint8]*MixingLayer),
	}
}

type MixingLayer struct {
	// the layer's priority over other layers (low = important, high = unimportant)
	Priority int
	// when covered decide what this layer will lower it's volume to
	Bed float32
	// the raw io reader
	raw io.Reader
	//  any edits that need to be donme to samples
	editor SampleEditor
	// buffer channel
	buffer chan []byte
	Ctx    context.Context
	cancel context.CancelFunc
}

func NewMixingLayer(priority int, bed float32, frameBuffer int, reader io.Reader, editor SampleEditor) *MixingLayer {
	ctx, cancel := context.WithCancel(context.Background())
	return &MixingLayer{
		Priority: priority,
		Bed:      bed,
		raw:      reader,
		editor:   editor,
		Ctx:      ctx,
		cancel:   cancel,
		buffer:   make(chan []byte, frameBuffer),
	}
}

type generalMixer struct {
	// the size of each frame
	frameCount int

	lock   sync.RWMutex
	layers map[uint8]*MixingLayer

	// reused snapshot by NextFrames func
	snapshot []idLayer
}

type idLayer struct {
	id    uint8
	layer *MixingLayer
}

func (m *generalMixer) Register(id uint8, layer *MixingLayer) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	_, ok := m.layers[id]
	if ok {
		return fmt.Errorf("layer of id %d already registered", id)
	}

	m.layers[id] = layer
	go layer.readLayerToBuffer(m.frameCount)
	return nil
}

func (m *generalMixer) Unregister(id uint8) {
	m.lock.Lock()
	defer m.lock.Unlock()

	layer, ok := m.layers[id]
	if ok {
		layer.cancel()
		delete(m.layers, id)
	}
}

func (m *generalMixer) CloseLayers() {
	m.lock.Lock()
	defer m.lock.Unlock()
	keys := maps.Keys(m.layers)
	for id := range keys {
		layer, ok := m.layers[id]
		if ok {
			layer.cancel()
			delete(m.layers, id)
		}
	}
}

func (m *generalMixer) NextFrames() ([]byte, error) {
	// copy layers to prevent lock holding for this n frames
	m.lock.RLock()
	var topPriority int
	m.snapshot = m.snapshot[:0]
	for id, layer := range m.layers {
		if layer.Priority > topPriority {
			topPriority = layer.Priority
		}

		m.snapshot = append(m.snapshot, idLayer{id: id, layer: layer})
	}
	m.lock.RUnlock()

	gallows := make([]uint8, 0)
	processed := make([]float32, m.frameCount*channels)

	for _, snap := range m.snapshot {
		id, layer := snap.id, snap.layer
		var bed float32
		if layer.Priority < topPriority {
			bed = layer.Bed
		} else {
			bed = 1
		}

		select {
		case next, ok := <-layer.buffer:
			if !ok {
				gallows = append(gallows, id)
				break
			}
			layer.processFrame(next, processed, bed)
		default:
			continue
		}
	}

	// evict
	if len(gallows) != 0 {
		m.lock.Lock()
		for _, id := range gallows {
			layer, ok := m.layers[id]
			if ok {
				layer.cancel()
				delete(m.layers, id)
			}
		}
		m.lock.Unlock()
	}

	out := int16ToByte(shrinkFloats(processed))
	return out, nil
}

func (l *MixingLayer) readLayerToBuffer(frameCount int) {
	defer close(l.buffer)
	for {
		buff := make([]byte, frameCount*channels*bytesPerSample)
		n, err := io.ReadFull(l.raw, buff)

		last := false
		switch {
		case err == nil:
		case errors.Is(err, io.ErrUnexpectedEOF):
			clear(buff[n:])
			last = true
		default:
			return
		}

		select {
		case l.buffer <- buff:
		case <-l.Ctx.Done():
			return
		}

		if last {
			return
		}

	}
}

func (l *MixingLayer) processFrame(raw []byte, joined []float32, bed float32) {
	asFloat := normalizeInts(ByteToInt16(raw))
	for i := range asFloat {
		joined[i] += l.editor(asFloat[i]) * bed
	}
}
