package index

import (
	"bytes"
	"hades/data"
	"math/rand"
	"sort"
	"sync"
)


const (
	// Maximum number of levels in a SkipList
	maxLevel int16 = 18
	// Probability of increasing the level
	probability float32 = 0.5
)

type Node struct {
	key     []byte
	value   interface{}
	forward []*Node // 各层的下一个指针
}

type SkipList struct {
	head   *Node
	level  int16
	length int
	lock   *sync.RWMutex
}

func newNode(key []byte, value interface{}, level int16) *Node {
	return &Node{
		key:     key,
		value:   value,
		forward: make([]*Node, level),
	}
}

func NewSkipList() *SkipList {
	head := newNode(nil, nil, maxLevel)
	return &SkipList{
		head:   head,
		level:  1,
		length: 0,
		lock:   new(sync.RWMutex),
	}
}

func randomLevel() int16 {
	level := int16(1)
	for float32(rand.Int31()&0xFFFF) < (probability*0xFFFF) && level < maxLevel {
		level++
	}
	return maxLevel
}

// Put 向索引中存储 key 对应的数据位置信息, 如果键已存在，更新值并返回旧值
func (sl *SkipList) Put(key []byte, pos *data.LogRecordPos) *data.LogRecordPos {
	sl.lock.Lock()
	defer sl.lock.Unlock()

	update := make([]*Node, maxLevel) // 记录每层需要更新的节点
	current := sl.head

	// 从最高层开始查找 【关键代码】
	for i := sl.level - 1; i >= 0; i-- {
		// 在当前层查找插入位置
		for current.forward[i] != nil && bytes.Compare(current.forward[i].key, key) < 0 {
			current = current.forward[i] // current.forward[i].key < key
		}
		update[i] = current
	}

	if current.forward[0] != nil && bytes.Equal(current.forward[0].key, key) {
		// 如果键已存在，更新值并返回旧值
		oldVal := current.forward[0].value
		current.forward[0].value = pos
		return oldVal.(*data.LogRecordPos)
	}

	level := randomLevel()
	if level > sl.level {
		// 如果新节点的层数大于当前层数，需要更新 update 切片
		for i := sl.level; i < level; i++ {
			update[i] = sl.head
		}
		sl.level = level
	}

	newNode := newNode(key, pos, level)
	for i := int16(0); i < level; i++ {
		// 更新节点的各层指针
		newNode.forward[i] = update[i].forward[i]
		update[i].forward[i] = newNode
	}
	sl.length++
	return nil
}

func (sl *SkipList) Get(key []byte) *data.LogRecordPos {
	sl.lock.RLock()
	defer sl.lock.RUnlock()

	current := sl.head

	for i := sl.level - 1; i >= 0; i-- {
		// 在当前层查找键值对
		for current.forward[i] != nil && bytes.Compare(current.forward[i].key, key) < 0 {
			current = current.forward[i]
		}
		if current.forward[i] != nil && bytes.Equal(current.forward[i].key, key) {
			return current.forward[i].value.(*data.LogRecordPos)
		}
	}
	return nil
}

// Delete 根据 key 删除对应的索引位置信息, 如果键不存在，返回 false
func (sl *SkipList) Delete(key []byte) (*data.LogRecordPos, bool) {
	sl.lock.Lock()
	defer sl.lock.Unlock()

	update := make([]*Node, maxLevel)
	current := sl.head

	for i := sl.level - 1; i >= 0; i-- {
		// 在当前层查找要删除的节点
		for current.forward[i] != nil && bytes.Compare(current.forward[i].key, key) < 0 {
			current = current.forward[i]
		}
		update[i] = current
	}

	if current.forward[0] != nil && bytes.Equal(current.forward[0].key, key) {
		// 找到要删除的节点并更新指针
		target := current.forward[0]
		for i := int16(0); i < sl.level; i++ {
			if update[i].forward[i] != target {
				break
			}
			update[i].forward[i] = target.forward[i]
		}
		sl.length--
		return target.value.(*data.LogRecordPos), true
	}

	return nil, false
}

func (sl *SkipList) Size() int {
	sl.lock.RLock()
	defer sl.lock.RUnlock()
	return sl.length
}



func (sl *SkipList) Iterator(reverse bool) Iterator {
	sl.lock.RLock()
	defer sl.lock.RUnlock()
	return newSkipListIterator(sl, reverse)
}

func (sl *SkipList) Close() error {
	return nil
}

type skiplistIterator struct {
	currIndex int     // 当前遍历的下标位置
	reverse   bool    //	是否反向遍历
	values    []*Item //key + 位置索引信息
}

func newSkipListIterator(sl *SkipList, reverse bool) *skiplistIterator {

	values := make([]*Item, sl.Size())
	current := sl.head
	current = current.forward[0]

	for current != nil {
		item := &Item{
			key: current.key,
			pos: current.value.(*data.LogRecordPos),
		}
		values = append(values, item)
	}

	reverseSlice := func(slice []*Item) {
		for i, j := 0, len(slice)-1; i < j; i, j = i+1, j-1 {
			slice[i], slice[j] = slice[j], slice[i]
		}
	}

	if reverse {
		reverseSlice(values)
	}
	return &skiplistIterator{
		currIndex: 0,
		reverse:   reverse,
		values:    values,
	}
}

func (s *skiplistIterator) Next() {
	s.currIndex += 1
}

func (s *skiplistIterator) Rewind() {
	s.currIndex = 0
}

func (s *skiplistIterator) Seek(key []byte) {
	if s.reverse {
		s.currIndex = sort.Search(len(s.values), func(i int) bool {
			return bytes.Compare(s.values[i].key, key) <= 0
		})
	} else {
		s.currIndex = sort.Search(len(s.values), func(i int) bool {
			return bytes.Compare(s.values[i].key, key) >= 0
		})
	}
}

func (s *skiplistIterator) Valid() bool {
	return s.currIndex < len(s.values)
}

func (s skiplistIterator) Key() []byte {
	return s.values[s.currIndex].key
}

func (s skiplistIterator) Value() *data.LogRecordPos {
	return s.values[s.currIndex].pos
}

func (s skiplistIterator) Close() {
	s.values = nil
}
