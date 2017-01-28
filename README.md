# Go Tor Crawler

It get the site list and download each site contents to a directory called "sites".

# How to

1. Start the Tor proxy:  
> docker run -it -p 8118:8118 -p 9050:9050 dperson/torproxy  

2. Build and start the crawler:  
> go get github.com/PuerkitoBio/goquery  
> go get github.com/metal3d/go-slugify  
> go get golang.org/x/net/proxy  
> go install  
> go-tor-crawler config.json  

3. Go make a coffee, the files will be saved in "sites" directory.  
