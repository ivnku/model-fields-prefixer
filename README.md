# Model Fields Prefixer

### Introduction

If you've ever faced a situation where you were using tools like sqlx and wondered how to map the entire result to a struct with an indefinite number of nested models then this  library is for you.

In [sqlx](https://github.com/jmoiron/sqlx) you need to specify prefixes in a query for your db columns and those prefixes must exactly match db tags of your inner structs. Let's imagine you have the following structures:

```golang
type User struct {
	ID      uuid.UUID      `json:"id" db:"id"`
	Name    sql.NullString `json:"name" db:"name"`
	Address Address        `json:"userAdditions" db:"ua"`
}

type Address struct {
	ID           uuid.UUID     `json:"id" db:"id"`
	City         string        `json:"city" db:"city"`
	LocationMeta *LocationMeta `json:"location_meta" db:"lm"`
}

type LocationMeta struct {
	ID        uuid.UUID `json:"id" db:"id"`
	ExtraData string    `json:"data" db:"extra_data"`
}
```

And you want to query for a user with specific id, joining "addresses" and "location_meta" tables. You would do something like this:

**query**

```SQL
SELECT * FROM users
JOIN addresses a ON users.address_id=a.id
JOIN location_meta lm ON a.location_meta_id=lm.id
WHERE users.id=$1
```

**repository**

```golang
var user models.User

err := r.db.GetContext(ctx, &user, userGetByID, id)
if err != nil {
	return nil, err
}

return &user, nil
```

You will get user's data but the problem is that inner structs (Address, LocationMeta) will be empty, because sqlx can't map the results from such a query. You can make sqlx map the results if you will rewrite your query like this:

```sql
SELECT
    users.id,
    users.name,
    a.id AS "addr.id",
    a.city AS "addr.city",
    lm.id AS "addr.lm.id",
    lm.extra_data AS "addr.lm.extra_data"
FROM users
JOIN addresses a ON users.address_id=a.id
JOIN location_meta lm ON a.location_meta_id=lm.id
WHERE users.id=$1
```

As you can see there is a lot of unnecessary code in the columns section and it's pretty easy to get confused by chains of aliases after "AS" keyword. And these are just tiny structs, just imagine how much code you'd have to write for bigger ones.

### Usage

And here Model Fields Prefixer comes to the rescue. This library helps to create columns list with prefixes based on passed model and to insert it to specific sql query, e.g.:

```golang
var user models.User

m := mfp.NewModelFieldsPrefixer()

err := r.db.GetContext(
    ctx,
    &user,
    m.Columns(User{}, "u", mfp.M{N: "Addresses", A: "addr"}).WithinQuery(userGetByID),
    id,
)
if err != nil {
	return nil, err
}

return &user, nil
```

The example above creates the full list of columns with necessary prefixes to map result in User model and in Address inner model. First you need to create an instance of Model Fields Prefixer - `m := mfp.NewModelFieldsPrefixer()` then method `Columns(model any, dbTableAlias string, joinModels ...M) *ModelFieldsPrefixer` generates the list of columns, you must pass as arguments specific model and its db alias which you will use in a query, you also can specify inner models `M{}` of any level of nesting, where `M.N` means the name of a model and `M.A` its db alias. If you don't specify any additional models it means you want recursively get all models inside that parent model. If we want all the user data in the example above it will go like `m.Columns(User{}, "u").WithinQuery(userGetByID)`. Function `WithinQuery(query string) string` replaces `{columns}` placeholder in your query, so your sql query must look like `SELECT {columns} FROM users`

If you need to add some custom fields to a query, like aggregation functions and so on, you can use `CustomColumns(custom string) *ModelFieldsPrefixer`

### Improving performance

This library works using reflect package of Golang. To reduce scanning the same models more than one time we use caching. Also structs that don't have any db tags are going to "exclude list" and are not scanned in the next requests. Thus, you better use Model Fields Prefixer as a singletone, instead of creating its instance per every repository method.

### Concurrent access

If you have the Model Fields Prefixer instance injected in your repository and you have the code that invoke prefixer in different goroutines concurrently then you need to allocate a new instance of the prefixer in every such method - `func (mp *ModelFieldsPrefixer) AllocPrefixer() *ModelFieldsPrefxer`. It will create a new instance but keep the cache and the exclude list of the parent prefixer.
