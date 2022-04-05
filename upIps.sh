cat $HOME/chinaOk.txt|grep -E '[\d\.:]+' >>dhTrackers.txt
cat $HOME/ips.txt|grep -E '[\d\.:]+' >>dhTrackers.txt
cat dhTrackers.txt|sort -u|uniq > dhTrackers1.txt
mv dhTrackers1.txt dhTrackers.txt
rm -rf $HOME/chinaOk.txt  $HOME/ips.txt
