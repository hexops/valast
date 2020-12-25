package test

type foo struct {
	bar string
}

func (f *foo) String() string {
	return "foo.String says hi"
}

func NewFoo() *foo {
	return &foo{
		bar: "hello2",
	}
}

type Bazer interface {
	Baz() (err error)
}

type Baz struct {
	Bam  complex64
	zeta *foo
	Beta interface{}
}

func (b *Baz) String() string {
	return "Baz.String says hi"
}

func (b *Baz) Baz() (err error) {
	return nil
}

func NewBaz() *Baz {
	return &Baz{
		Bam: 1.34,
		zeta: &foo{
			bar: "hello",
		},
	}
}
