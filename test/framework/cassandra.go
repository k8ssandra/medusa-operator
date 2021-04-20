package framework

import (
	"fmt"
	"k8s.io/apimachinery/pkg/util/rand"
	"strconv"
	"strings"
	"testing"
)

type User struct {
	Email string
	Name  string
}

type CassandraSchemaManager struct {
	t         *testing.T
	namespace string
	keyspace  string
	table     string
}

func NewCassandraSchemaManager(t *testing.T, namespace string) *CassandraSchemaManager {
	return &CassandraSchemaManager{
		t:         t,
		namespace: namespace,
		keyspace:  "medusa_" + rand.String(6),
		table:     "test_" + rand.String(6),
	}
}

func (c *CassandraSchemaManager) CreateKeyspace() error {
	c.t.Logf("creating keyspace %s", c.keyspace)

	cql := "create keyspace " + c.keyspace + " with replication = {'class': 'NetworkTopologyStrategy', 'dc1': 3}"
	pod := "medusa-test-dc1-default-sts-0"

	_, err := ExecCqlsh(c.t, c.namespace, pod, cql)
	return err
}

func (c *CassandraSchemaManager) CreateUsersTable() error {
	c.t.Logf("creating table %s.%s", c.keyspace, c.table)

	cql := fmt.Sprintf("create table %s.%s (email text primary key, name text)", c.keyspace, c.table)
	pod := "medusa-test-dc1-default-sts-0"

	_, err := ExecCqlsh(c.t, c.namespace, pod, cql)
	return err
}

func (c *CassandraSchemaManager) InsertRows(users []User) error {
	c.t.Log("inserting users")

	pod := "medusa-test-dc1-default-sts-0"
	for _, user := range users {
		cql := fmt.Sprintf("insert into %s.%s (email, name) values ('%s', '%s')", c.keyspace, c.table, user.Email, user.Name)
		if _, err := ExecCqlsh(c.t, c.namespace, pod, cql); err != nil {
			return err
		}
	}
	return nil
}

func (c *CassandraSchemaManager) RowCountMatches(count int) (bool, error) {
	pod := "medusa-test-dc1-default-sts-0"
	cql := fmt.Sprintf("select * from %s.%s", c.keyspace, c.table)

	if output, err := ExecCqlsh(c.t, c.namespace, pod, cql); err == nil {
		return strings.Contains(output, strconv.Itoa(count)+" rows"), nil
	} else {
		return false, err
	}
}
