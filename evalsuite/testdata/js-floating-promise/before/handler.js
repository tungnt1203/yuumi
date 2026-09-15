async function fetchUser(id) {
  const res = await fetch(`/api/users/${id}`);
  if (!res.ok) {
    throw new Error(`failed to fetch user ${id}`);
  }
  return res.json();
}

async function handleRequest(req, res) {
  try {
    const user = await fetchUser(req.params.id);
    res.json(user);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
}

module.exports = { fetchUser, handleRequest };
