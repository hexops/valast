package test

type ComplexNode struct {
	Left, Right *ComplexNode
	Child       *ComplexNodeChild
}

type ComplexNodeChild struct {
	Parent   *ComplexNode
	Child    *ComplexNodeChild
	Siblings []*ComplexNode
}
