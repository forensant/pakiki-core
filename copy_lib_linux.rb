#!/usr/bin/env ruby

copy_to = ARGV.shift
Dir.chdir(copy_to)
copied_files = true
while copied_files == true do
  puts "----------------"
  copied_files = false
  files_to_check = Dir.glob('*')

  files_to_check.each do |file|
    puts "Find out what #{file} requires"

    required_libs = `ldd #{file}`
    required_libs.each_line do |lib|
      file_path = lib.split[2]

      if file_path == nil || file_path.index('/lib/') == 0 then
	next
      end

      filename = file_path.split('/').last
      next if File.exist?(copy_to + filename) || !File.exist?(file_path)
      
      puts "Copying #{file_path} to #{copy_to}"
      `cp -L #{file_path} #{copy_to}`
      copied_files = true
    end
  end
end
