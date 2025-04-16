package webdav

const rootTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>WebDAV Shares</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            margin: 0 auto;
            padding: 20px;
        }
        h1 {
            color: #2c3e50;
        }
        ul {
            list-style: none;
            padding: 0;
        }
        li {
            margin: 10px 0;
        }
        a {
            color: #3498db;
            text-decoration: none;
            padding: 10px;
            display: block;
            border: 1px solid #eee;
            border-radius: 4px;
        }
        a:hover {
            background-color: #f7f9fa;
        }
    </style>
</head>
<body>
    <h1>Available WebDAV Shares</h1>
    <ul>
        {{range .Handlers}}
            <li><a href="{{$.Prefix}}{{.RootPath}}">{{$.Prefix}}{{.RootPath}}</a></li>
        {{end}}
    </ul>
</body>
</html>
`

const directoryTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Index of {{.Path}}</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            margin: 0 auto;
            padding: 20px;
        }
        h1 {
            color: #2c3e50;
        }
        ul {
            list-style: none;
            padding: 0;
        }
        li {
            margin: 10px 0;
        }
        a {
            color: #3498db;
            text-decoration: none;
            padding: 10px;
            display: block;
            border: 1px solid #eee;
            border-radius: 4px;
            position: relative;
            padding-left: 50px; /* Make room for the number */
        }
        a:hover {
            background-color: #f7f9fa;
        }
        .file-info {
            color: #666;
            font-size: 0.9em;
            float: right;
        }
        .parent-dir {
            background-color: #f8f9fa;
        }
        .file-number {
            position: absolute;
            left: 10px;
            top: 10px;
            width: 30px;
            color: #777;
            font-weight: bold;
            text-align: right;
        }
        .file-name {
            display: inline-block;
            max-width: 70%;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }
    </style>
</head>
<body>
    <h1>Index of {{.Path}}</h1>
    <ul>
        {{if .ShowParent}}
            <li><a href="{{urlpath .ParentPath}}" class="parent-dir"><span class="file-number"></span>Parent Directory</a></li>
        {{end}}
        {{range $index, $file := .Children}}
            <li>
                <a href="{{urlpath (printf "%s/%s" $.Path $file.Name)}}">
                    <span class="file-number">{{add $index 1}}.</span>
                    <span class="file-name">{{$file.Name}}{{if $file.IsDir}}/{{end}}</span>
                    <span class="file-info">
                        {{if not $file.IsDir}}
                            {{formatSize $file.Size}}
                        {{end}}
                        {{$file.ModTime.Format "2006-01-02 15:04:05"}}
                    </span>
                </a>
            </li>
        {{end}}
    </ul>
</body>
</html>
`
