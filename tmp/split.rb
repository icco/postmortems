#! /usr/bin/env ruby

require "rubygems"
require "bundler"
Bundler.require(:default)
require 'securerandom'

File.open("posts.md", "r") do |f|
  f.each_line do |line|
    data = line.strip
    id = SecureRandom.hex(8)
    if data.empty?
      next 
    end

    matches = /^\[(.+)\]\((.+)\)\. (.+)$/.match(data)
    body = <<-HEAD
---

url: ""
start_time: ""
end_time: ""
categories:
- postmortem
company: ""
product: ""

---

#{data}
HEAD

    if !matches.nil?
      body = <<-HEAD
---

url: "#{matches[2]}"
start_time: ""
end_time: ""
categories:
- postmortem
company: "#{matches[1]}"
product: ""

---

#{matches[3]}
HEAD
    end

    prsr = FrontMatterParser::Parser.new(:md)
    fmp = prsr.call(body)
    fm = fmp.front_matter
    p fm

    File.open("../data/#{id}.md", 'w') {|f| f.write(body) }
  end
end
