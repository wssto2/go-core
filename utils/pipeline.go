package utils

type PipelineContext struct {
	values map[string]any
}

func NewPipelineContext() *PipelineContext {
	return &PipelineContext{
		values: make(map[string]any),
	}
}

func (pc *PipelineContext) Set(key string, value any) {
	pc.values[key] = value
}

func (pc *PipelineContext) Get(key string) (any, bool) {
	value, exists := pc.values[key]
	return value, exists
}

func (pc *PipelineContext) Is(key string, value any) bool {
	v, exists := pc.values[key]
	if !exists {
		return false
	}
	return v == value
}

type PipelineStep[T any] func(*T, *PipelineContext) error

type Pipeline[T any] struct {
	subject *T
	steps   []PipelineStep[T]
	ctx     *PipelineContext
}

func NewPipeline[T any](subject *T) *Pipeline[T] {
	return &Pipeline[T]{
		subject: subject,
		steps:   []PipelineStep[T]{},
		ctx:     NewPipelineContext(),
	}
}

func (p *Pipeline[T]) WithContext(ctx *PipelineContext) *Pipeline[T] {
	p.ctx = ctx
	return p
}

func (p *Pipeline[T]) AddStep(step PipelineStep[T]) *Pipeline[T] {
	p.steps = append(p.steps, step)
	return p
}

func (p *Pipeline[T]) Run() error {
	for _, step := range p.steps {
		err := step(p.subject, p.ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Pipeline[T]) GetSubject() *T {
	return p.subject
}
