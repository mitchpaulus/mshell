
Absolutely! Here are **10 common jq usage examples** that cover a broad (orthogonal) range of what jq is typically used for. Each example is simple and includes an example input (as a JSON snippet) and the jq command that processes it, along with the expected output.

---

### 1. **Extract a field from an object**

**Input:**

```json
{"name":"Alice","age":30}
```

**jq:**

```bash
jq '.name'
```

**Output:**

```
"Alice"
```

---

### 2. **Select an element from an array**

**Input:**

```json
[1, 2, 3, 4]
```

**jq:**

```bash
jq '.[2]'
```

**Output:**

```
3
```

---

### 3. **Filter objects in an array by field value**

**Input:**

```json
[
  {"name":"Alice","active":true},
  {"name":"Bob","active":false}
]
```

**jq:**

```bash
jq '.[] | select(.active)'
```

**Output:**

```json
{"name":"Alice","active":true}
```

---

### 4. **Map (transform) all values in an array**

**Input:**

```json
[1, 2, 3]
```

**jq:**

```bash
jq 'map(. * 2)'
```

**Output:**

```json
[2,4,6]
```

---

### 5. **Extract multiple fields from an object**

**Input:**

```json
{"name":"Alice","age":30,"city":"NYC"}
```

**jq:**

```bash
jq '{name, city}'
```

**Output:**

```json
{"name":"Alice","city":"NYC"}
```

---

### 6. **Flatten a nested array**

**Input:**

```json
[[1,2],[3,4]]
```

**jq:**

```bash
jq 'flatten'
```

**Output:**

```json
[1,2,3,4]
```

---

### 7. **Change all keys to uppercase**

**Input:**

```json
{"a":1,"b":2}
```

**jq:**

```bash
jq 'with_entries(.key |= ascii_upcase)'
```

**Output:**

```json
{"A":1,"B":2}
```

---

### 8. **Sum up numeric values in an array**

**Input:**

```json
[1,2,3,4]
```

**jq:**

```bash
jq 'add'
```

**Output:**

```
10
```

---

### 9. **Read from a deeply nested structure**

**Input:**

```json
{"user":{"profile":{"email":"a@b.com"}}}
```

**jq:**

```bash
jq '.user.profile.email'
```

**Output:**

```
"a@b.com"
```

---

### 10. **Group objects by a property**

**Input:**

```json
[
  {"type":"cat","name":"Whiskers"},
  {"type":"dog","name":"Rover"},
  {"type":"cat","name":"Fluffy"}
]
```

**jq:**

```bash
jq 'group_by(.type)'
```

**Output:**

```json
[
  [
    {"type":"cat","name":"Whiskers"},
    {"type":"cat","name":"Fluffy"}
  ],
  [
    {"type":"dog","name":"Rover"}
  ]
]
```

---

**Let me know if you want these as a table, with more explanations, or with an emphasis on more advanced use-cases!**
