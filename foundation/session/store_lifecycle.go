package session

// Close 关闭并移除活跃会话，返回是否确实移除了一个。
func (s *Store) Close(id string) bool {
	s.mu.Lock()
	session, found := s.sessions[id]
	if found {
		delete(s.sessions, id)
	}
	s.mu.Unlock()

	if !found {
		return false
	}
	session.Close()
	return true
}

// Remove 关闭会话并删除其持久化数据，返回是否有东西被处理。
func (s *Store) Remove(id string) (bool, error) {
	closed := s.Close(id)
	persistence := s.persistence
	if persistence == nil {
		return closed, nil
	}
	if err := persistence.Remove(id); err != nil {
		return closed, err
	}
	return true, nil
}

// PersistedIDs 返回已持久化的会话 id（未接持久化时为空）。
func (s *Store) PersistedIDs() ([]string, error) {
	persistence := s.persistence
	if persistence == nil {
		return nil, nil
	}
	return persistence.List()
}

// attach 把仓库接到会话的生命周期上：追加即落盘、关闭即出活跃集。
func (s *Store) attach(session *Session) {
	persistence := s.persistence
	if persistence != nil {
		session.OnEvent(func(event Event) {
			if err := persistence.Append(session.ID(), event); err != nil {
				s.errors.report(err)
			}
		})
	}
	session.OnClose(func() { s.forget(session.ID()) })
}

func (s *Store) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *Store) persistSeed(persistence Persistence, id string, events []Event) {
	if persistence == nil {
		return
	}
	for _, event := range events {
		if err := persistence.Append(id, event); err != nil {
			s.errors.report(err)
		}
	}
}

func loadSeed(persistence Persistence, id string) ([]Event, error) {
	if persistence == nil {
		return nil, nil
	}
	return persistence.Load(id)
}
