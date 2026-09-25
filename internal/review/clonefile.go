package review

import "os"

// readCloneFile đọc file name (tương đối với gốc repo) trong thư mục clone
// dir. File trong clone do tác giả PR kiểm soát và được đọc trên máy
// server, nên đi qua os.Root: đường dẫn hay symlink trỏ ra ngoài dir (vd
// .yuumi.yml -> /proc/self/environ) bị từ chối thay vì bị đọc rồi đưa vào
// prompt hay comment. Symlink trỏ vào file khác TRONG dir vẫn đọc được.
//
// Không có file thì lỗi thoả errors.Is(err, fs.ErrNotExist).
func readCloneFile(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(name)
}
