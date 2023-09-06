# go-network-file
Provides an io.ReaderAt and io.WriterAt interface over the network using simple HTTP calls.

## Installation

```
go get gopkg.in/vansante/go-network-file.v2
```

## Documentation

https://pkg.go.dev/gopkg.in/vansante/go-network-file.v2


## Basic usage

To start a file server, do:

```golang
srv := NewFileServer("/my-files", "mySecretCode")
err := http.ListenAndServe(":8080", srv)
if err != nil {
    panic(err)
}
```

Now we want to create a file that we can write to remotely:

```golang
file, err := os.CreateTemp(os.TempDir(), "New Test File.txt")
assert.NoError(t, err)
defer func() {
    _ = file.Close()
}()

ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute)
defer cancelFn()

err = srv.ServeFileWriter(ctx, fileID, dst)
if err != nil {
    panic(err)
}
```

Then on the client side, we open a writer for this file:

```golang
ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute)
defer cancelFn()

wrtr := NewWriter(ctx, "http://my-file-server:8080/my-files", "mySecretCode", fileID)
_, err := wrtr.Write([]byte("Hello, World!"))
if err != nil {
    panic(err)
}
err = wrtr.Close()
if err != nil {
    panic(err)
}
```